import { ManagementIconButton } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type {
  NetworkMihomoDNSMode,
  NetworkMihomoManualProtocol,
  NetworkMihomoMode,
  NetworkMihomoProfile,
  NetworkMihomoProfileInput,
  NetworkMihomoProfileStatus,
  NetworkMihomoSourceType,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
} from 'antd'
import { useState } from 'react'
import { networkAccessMutations } from './mutations'
import './proxy.css'
import { networkAccessQueries } from './queries'
import { TablePane } from './table-pane'
interface NetworkMihomoPaneProps {
  canCreate: boolean
  canDelete: boolean
  canUpdate: boolean
}
interface MihomoFormValues {
  bypassCidrsText?: string
  bypassHostsText?: string
  controllerPort: number
  deviceId: string
  dnsMode: NetworkMihomoDNSMode
  failClosed: boolean
  fakeIpRange?: string
  manualPassword?: string
  manualPort?: number
  manualProtocol?: NetworkMihomoManualProtocol
  manualServer?: string
  manualUsername?: string
  mixedPort: number
  mode: NetworkMihomoMode
  name: string
  selectedProxy?: string
  selectorGroup: string
  sourceType?: NetworkMihomoSourceType
  status: NetworkMihomoProfileStatus
  subscriptionUrl?: string
}
const LIST_FILTER = { limit: 200 } as const
const modeLabel = (mode: NetworkMihomoMode) => (mode === 'managed_follow' ? '跟随模式' : 'App 自选')
function textList(value?: string) {
  return String(value ?? '')
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}
export function NetworkMihomoPane({ canCreate, canDelete, canUpdate }: NetworkMihomoPaneProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const profiles = useQuery(networkAccessQueries.mihomoProfiles(LIST_FILTER))
  const visibleProfilesData = profiles.isEnabled === false ? undefined : readableQueryData(profiles)
  const createProfile = useOperationMutation(
    networkAccessMutations.mihomoProfiles.create(queryClient),
    { label: ['创建 代理隧道', 'Create proxy profile'], target: (v) => v.name },
  )
  const updateProfile = useOperationMutation(
    networkAccessMutations.mihomoProfiles.update(queryClient),
    { label: ['保存 代理隧道', 'Save proxy profile'], target: (v) => v.profileId },
  )
  const deleteProfile = useOperationMutation(
    networkAccessMutations.mihomoProfiles.remove(queryClient),
    { label: ['删除 代理隧道', 'Delete proxy profile'], target: (v) => v },
  )
  const operationBlocked =
    createProfile.blocked ||
    updateProfile.blocked ||
    deleteProfile.blocked ||
    (profiles.isEnabled !== false && queryAccessDenied(profiles))
  const [editor, setEditor] = useState<NetworkMihomoProfile | 'create' | null>(null)
  const [form] = Form.useForm<MihomoFormValues>()
  function openCreate() {
    if (operationBlocked) return
    form.resetFields()
    form.setFieldsValue({
      mode: 'managed_follow',
      sourceType: 'managed_subscription',
      status: 'active',
      mixedPort: 7890,
      controllerPort: 9090,
      dnsMode: 'fake_ip',
      fakeIpRange: '198.18.0.0/15',
      selectorGroup: 'PROXY',
      manualProtocol: 'https',
      manualPort: 443,
      failClosed: true,
    })
    setEditor('create')
  }
  function openEdit(profile: NetworkMihomoProfile) {
    if (operationBlocked) return
    form.resetFields()
    form.setFieldsValue({
      ...profile,
      sourceType: profile.sourceType ?? 'managed_subscription',
      subscriptionUrl: undefined,
      manualPassword: undefined,
      manualPort: undefined,
      manualProtocol: undefined,
      manualServer: undefined,
      manualUsername: undefined,
      bypassCidrsText: profile.bypassCidrs.join('\n'),
      bypassHostsText: profile.bypassHosts.join('\n'),
    })
    setEditor(profile)
  }
  function closeEditor() {
    setEditor(null)
    form.resetFields()
  }
  function validateManualRequired(value: unknown) {
    const manualValues = form.getFieldsValue([
      'manualProtocol',
      'manualServer',
      'manualPort',
      'manualUsername',
      'manualPassword',
    ])
    const changed = Object.values(manualValues).some((item) => String(item ?? '').trim())
    const preservesExisting =
      editor !== 'create' && editor?.sourceType === 'manual_node' && editor.manualNodeConfigured
    return String(value ?? '').trim() || (preservesExisting && !changed)
      ? Promise.resolve()
      : Promise.reject(new Error(t('networkAccess.form.required', '必填项')))
  }
  async function submit() {
    const values = await form.validateFields()
    const subscriptionUrl = values.subscriptionUrl?.trim()
    const manualServer = values.manualServer?.trim()
    const manualUsername = values.manualUsername?.trim()
    const manualNode =
      values.mode === 'managed_follow' &&
      values.sourceType === 'manual_node' &&
      values.manualProtocol &&
      manualServer &&
      values.manualPort
        ? {
            protocol: values.manualProtocol,
            server: manualServer,
            port: values.manualPort,
            ...(manualUsername
              ? { username: manualUsername, password: values.manualPassword }
              : {}),
          }
        : undefined
    const input: NetworkMihomoProfileInput = {
      deviceId: values.deviceId.trim(),
      name: values.name.trim(),
      mode: values.mode,
      status: values.status,
      mixedPort: values.mixedPort,
      controllerPort: values.controllerPort,
      dnsMode: values.dnsMode,
      selectorGroup: values.selectorGroup.trim(),
      bypassCidrs: textList(values.bypassCidrsText),
      bypassHosts: textList(values.bypassHostsText),
      failClosed: values.mode === 'managed_follow' ? true : Boolean(values.failClosed),
      ...(values.mode === 'managed_follow' ? { sourceType: values.sourceType } : {}),
      ...(values.mode === 'managed_follow' &&
      values.sourceType === 'managed_subscription' &&
      subscriptionUrl
        ? { subscriptionUrl }
        : {}),
      ...(values.mode === 'managed_follow' &&
      values.sourceType === 'managed_subscription' &&
      values.selectedProxy?.trim()
        ? { selectedProxy: values.selectedProxy.trim() }
        : {}),
      ...(manualNode ? { manualNode } : {}),
      ...(values.dnsMode === 'fake_ip' && values.fakeIpRange?.trim()
        ? { fakeIpRange: values.fakeIpRange.trim() }
        : {}),
    }
    if (editor === 'create') await createProfile.mutateAsync(input)
    else if (editor) await updateProfile.mutateAsync({ profileId: editor.id, input })
    closeEditor()
  }
  const columns: TableColumnsType<NetworkMihomoProfile> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name' },
    {
      title: t('networkAccess.mihomo.engine', '引擎'),
      key: 'engine',
      width: 100,
      render: () => 'mihomo',
    },
    { title: t('networkAccess.deviceId', '设备 ID'), dataIndex: 'deviceId' },
    {
      title: t('networkAccess.mihomo.mode', '运行模式'),
      dataIndex: 'mode',
      width: 120,
      render: modeLabel,
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 100,
      render: (value: string) => (
        <StatusTag
          value={value}
          label={
            value === 'active'
              ? t('networkAccess.mihomo.active', '启用')
              : t('networkAccess.mihomo.disabled', '停用')
          }
        />
      ),
    },
    {
      title: t('networkAccess.mihomo.source', '连接来源'),
      key: 'source',
      width: 120,
      render: (_, profile) =>
        profile.mode === 'app_subscription'
          ? t('networkAccess.mihomo.appSource', 'App 自选')
          : profile.sourceType === 'manual_node'
            ? t('networkAccess.mihomo.manualNode', '手动节点')
            : t('networkAccess.mihomo.managedSubscription', '订阅导入'),
    },
    { title: t('networkAccess.mihomo.selectedProxy', '代理节点'), dataIndex: 'selectedProxy' },
    {
      title: t('networkAccess.mihomo.rules', '分流规则'),
      key: 'rules',
      width: 100,
      render: (_, profile) => profile.bypassCidrs.length + profile.bypassHosts.length,
    },
    { title: t('networkAccess.mihomo.revision', '修订'), dataIndex: 'revision', width: 90 },
    ...(canUpdate || canDelete
      ? [
          {
            key: 'actions',
            width: 96,
            render: (_: unknown, profile: NetworkMihomoProfile) => (
              <Space size={4}>
                {canUpdate ? (
                  <ManagementIconButton
                    aria-label={t('networkAccess.mihomo.edit', '编辑 mihomo 配置')}
                    icon={<EditOutlined />}
                    tooltip={t('networkAccess.mihomo.edit', '编辑 mihomo 配置')}
                    onClick={() => openEdit(profile)}
                    disabled={operationBlocked}
                  />
                ) : null}
                {canDelete ? (
                  <Popconfirm
                    title={t('networkAccess.mihomo.confirmDelete', '确认删除此 mihomo 配置？')}
                    onConfirm={() => deleteProfile.mutate(profile.id)}
                    okButtonProps={{ disabled: operationBlocked }}
                    disabled={operationBlocked}
                  >
                    <ManagementIconButton
                      aria-label={t('networkAccess.mihomo.delete', '删除 mihomo 配置')}
                      danger
                      icon={<DeleteOutlined />}
                      tooltip={t('networkAccess.mihomo.delete', '删除 mihomo 配置')}
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]
      : []),
  ]
  return (
    <div className="soha-proxy-page">
      {!(editor !== null && !(profiles.isEnabled !== false && queryAccessDenied(profiles))) && (
        <>
          {' '}
          {createProfile.feedback}
          {updateProfile.feedback}
          {deleteProfile.feedback}{' '}
        </>
      )}

      <h1 className="soha-proxy-sr-only">{t('networkAccess.mihomo.pageTitle', '代理隧道')}</h1>
      <Alert
        type="info"
        showIcon
        description={t(
          'networkAccess.mihomo.protectionBoundary',
          '受保护目标优先于直连列表和自选订阅。端点需声明 mihomo-policy-v1 才能运行代理；旧端点停用代理后需升级并重新登记能力。当前仅约束进入 App Mihomo 的流量。',
        )}
      />
      <TablePane
        columns={columns}
        items={visibleProfilesData}
        loading={profiles.isLoading}
        refreshing={profiles.isFetching}
        error={profiles.isEnabled === false ? null : profiles.error}
        onRefresh={() => void (profiles.isEnabled === false ? undefined : profiles.refetch())}
        searchPlaceholder={t('networkAccess.mihomo.search', '搜索名称、设备或节点')}
        getSearchValues={(item) => [
          item.name,
          item.deviceId,
          item.mode,
          item.status,
          item.sourceType,
          item.selectedProxy,
        ]}
        createAction={
          canCreate ? (
            <Button
              size="small"
              type="primary"
              icon={<PlusOutlined />}
              onClick={openCreate}
              disabled={operationBlocked}
            >
              {t('networkAccess.mihomo.create', '新增代理隧道')}
            </Button>
          ) : null
        }
      />
      <Modal
        width={680}
        style={{ top: 32 }}
        styles={{ body: { maxHeight: 'calc(100vh - 190px)', overflowY: 'auto' } }}
        destroyOnHidden
        mask={{ closable: false }}
        open={editor !== null && !(profiles.isEnabled !== false && queryAccessDenied(profiles))}
        title={
          editor === 'create'
            ? t('networkAccess.mihomo.create', '新增代理隧道')
            : t('networkAccess.mihomo.edit', '编辑代理隧道')
        }
        confirmLoading={createProfile.isPending || updateProfile.isPending}
        okText={t('common.save', '保存')}
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) closeEditor()
        }}
        onOk={() => void submit()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createProfile.feedback}
        {updateProfile.feedback}
        {deleteProfile.feedback}

        <Form form={form} layout="vertical" disabled={operationBlocked}>
          <Divider titlePlacement="start" plain>
            {t('networkAccess.mihomo.basic', '基本配置')}
          </Divider>
          <Form.Item
            name="deviceId"
            label={t('networkAccess.deviceId', '设备 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="name"
            label={t('networkAccess.name', '名称')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item
            name="mode"
            label={t('networkAccess.mihomo.mode', '运行模式')}
            rules={[{ required: true }]}
          >
            <Select
              options={(['managed_follow', 'app_subscription'] as NetworkMihomoMode[]).map(
                (value) => ({ value, label: modeLabel(value) }),
              )}
              onChange={(mode: NetworkMihomoMode) => {
                if (mode === 'managed_follow') form.setFieldValue('failClosed', true)
              }}
            />
          </Form.Item>
          <Form.Item
            name="status"
            label={t('networkAccess.status', '状态')}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: 'active', label: t('networkAccess.mihomo.active', '启用') },
                { value: 'disabled', label: t('networkAccess.mihomo.disabled', '停用') },
              ]}
            />
          </Form.Item>
          <Divider titlePlacement="start" plain>
            {t('networkAccess.mihomo.source', '连接来源')}
          </Divider>
          <Form.Item noStyle shouldUpdate={(previous, current) => previous.mode !== current.mode}>
            {({ getFieldValue }) =>
              getFieldValue('mode') === 'managed_follow' ? (
                <>
                  <Form.Item name="sourceType" rules={[{ required: true }]}>
                    <Segmented
                      disabled={operationBlocked}
                      block
                      options={[
                        {
                          value: 'managed_subscription',
                          label: t('networkAccess.mihomo.managedSubscription', '订阅导入'),
                        },
                        {
                          value: 'manual_node',
                          label: t('networkAccess.mihomo.manualNode', '手动节点'),
                        },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item
                    noStyle
                    shouldUpdate={(previous, current) => previous.sourceType !== current.sourceType}
                  >
                    {({ getFieldValue: getSourceFieldValue }) =>
                      getSourceFieldValue('sourceType') === 'manual_node' ? (
                        <>
                          <Space size={12} style={{ width: '100%' }} align="start">
                            <Form.Item
                              name="manualProtocol"
                              label={t('networkAccess.mihomo.protocol', '协议')}
                              rules={[{ validator: (_, value) => validateManualRequired(value) }]}
                            >
                              <Select
                                style={{ width: 120 }}
                                options={(['http', 'https', 'socks5'] as const).map((value) => ({
                                  value,
                                  label: value.toUpperCase(),
                                }))}
                              />
                            </Form.Item>
                            <Form.Item
                              name="manualServer"
                              label={t('networkAccess.mihomo.server', '地址')}
                              extra={
                                editor !== 'create' && editor?.manualNodeConfigured
                                  ? t(
                                      'networkAccess.mihomo.manualPreserve',
                                      '全部留空即保留现有节点',
                                    )
                                  : undefined
                              }
                              rules={[{ validator: (_, value) => validateManualRequired(value) }]}
                            >
                              <Input maxLength={253} placeholder="proxy.example.com" />
                            </Form.Item>
                            <Form.Item
                              name="manualPort"
                              label={t('networkAccess.mihomo.port', '端口')}
                              rules={[{ validator: (_, value) => validateManualRequired(value) }]}
                            >
                              <InputNumber min={1} max={65535} style={{ width: 110 }} />
                            </Form.Item>
                          </Space>
                          <Space size={12} style={{ width: '100%' }} align="start">
                            <Form.Item
                              name="manualUsername"
                              label={t('networkAccess.mihomo.username', '用户名（可选）')}
                            >
                              <Input maxLength={256} autoComplete="off" />
                            </Form.Item>
                            <Form.Item
                              name="manualPassword"
                              label={t('networkAccess.mihomo.password', '密码（可选）')}
                              dependencies={['manualUsername']}
                              rules={[
                                ({ getFieldValue: getCredentialField }) => ({
                                  validator: (_, value) =>
                                    Boolean(getCredentialField('manualUsername')) === Boolean(value)
                                      ? Promise.resolve()
                                      : Promise.reject(
                                          new Error(
                                            t(
                                              'networkAccess.mihomo.credentialsTogether',
                                              '用户名和密码需同时填写',
                                            ),
                                          ),
                                        ),
                                }),
                              ]}
                            >
                              <Input.Password autoComplete="new-password" maxLength={4096} />
                            </Form.Item>
                          </Space>
                        </>
                      ) : (
                        <>
                          <Form.Item
                            name="subscriptionUrl"
                            label={t('networkAccess.mihomo.subscriptionUrl', '订阅 URL（只写）')}
                            extra={
                              editor !== 'create'
                                ? t(
                                    'networkAccess.mihomo.subscriptionPreserve',
                                    '留空即保留现有订阅',
                                  )
                                : t(
                                    'networkAccess.mihomo.subscriptionWrapped',
                                    'Soha 加密保存并封装下发给授权终端',
                                  )
                            }
                            rules={[
                              {
                                validator: (_, value) =>
                                  !String(value ?? '').trim() &&
                                  (editor === 'create' ||
                                    editor?.sourceType === 'manual_node' ||
                                    !editor?.subscriptionConfigured)
                                    ? Promise.reject(
                                        new Error(t('networkAccess.form.required', '必填项')),
                                      )
                                    : Promise.resolve(),
                              },
                            ]}
                          >
                            <Input.Password autoComplete="new-password" maxLength={4096} />
                          </Form.Item>
                          <Form.Item
                            name="selectedProxy"
                            label={t('networkAccess.mihomo.selectedProxy', '默认代理节点')}
                            rules={[{ required: true, whitespace: true }]}
                          >
                            <Input maxLength={128} />
                          </Form.Item>
                        </>
                      )
                    }
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
          <Divider titlePlacement="start" plain>
            {t('networkAccess.mihomo.execution', '执行策略')}
          </Divider>
          <Space size={12} style={{ width: '100%' }} align="start">
            <Form.Item
              name="mixedPort"
              label={t('networkAccess.mihomo.mixedPort', '混合端口')}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={65535} />
            </Form.Item>
            <Form.Item
              name="controllerPort"
              label={t('networkAccess.mihomo.controllerPort', '控制端口')}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={65535} />
            </Form.Item>
          </Space>
          <Form.Item
            name="dnsMode"
            label={t('networkAccess.mihomo.dnsMode', 'DNS 模式')}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: 'fake_ip', label: 'Fake-IP' },
                { value: 'disabled', label: t('networkAccess.mihomo.dnsDisabled', '关闭') },
              ]}
            />
          </Form.Item>
          <Form.Item
            noStyle
            shouldUpdate={(previous, current) => previous.dnsMode !== current.dnsMode}
          >
            {({ getFieldValue }) =>
              getFieldValue('dnsMode') === 'fake_ip' ? (
                <Form.Item
                  name="fakeIpRange"
                  label={t('networkAccess.mihomo.fakeIpRange', 'Fake-IP 网段')}
                  rules={[{ required: true, whitespace: true }]}
                >
                  <Input maxLength={32} />
                </Form.Item>
              ) : null
            }
          </Form.Item>
          <Form.Item
            name="selectorGroup"
            label={t('networkAccess.mihomo.selectorGroup', '策略组')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="bypassCidrsText"
            label={t('networkAccess.mihomo.bypassCidrs', '直连 CIDR')}
          >
            <Input.TextArea
              rows={3}
              placeholder={t('networkAccess.mihomo.oneRulePerLine', '每行一条规则')}
            />
          </Form.Item>
          <Form.Item
            name="bypassHostsText"
            label={t('networkAccess.mihomo.bypassHosts', '直连主机')}
          >
            <Input.TextArea
              rows={3}
              placeholder={t('networkAccess.mihomo.oneRulePerLine', '每行一条规则')}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(previous, current) => previous.mode !== current.mode}>
            {({ getFieldValue }) => (
              <Form.Item
                name="failClosed"
                label={t('networkAccess.mihomo.failClosed', '故障时阻断')}
                valuePropName="checked"
              >
                <Switch disabled={operationBlocked || getFieldValue('mode') === 'managed_follow'} />
              </Form.Item>
            )}
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
