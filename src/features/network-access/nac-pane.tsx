import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import type {
  NetworkAccessDeviceType,
  NetworkAccessMedium,
  NetworkAccessProfile,
  NetworkNASBinding,
  NetworkNASBindingInput,
  NetworkNASBindingStatus,
  NetworkEAPServerTrust,
  NetworkSession,
  NetworkSessionAction,
  NetworkSessionActionPlan,
  NetworkSiteProfileBinding,
  NetworkSiteProfileBindingInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Descriptions,
  Divider,
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
import { useState } from 'react'
import { formatDateTime } from '@/utils/time'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'
import { TablePane } from './table-pane'
import { responseLabel } from './endpoint-response/labels'

const LIST_FILTER = { limit: 200 } as const
const ACCESS_PROFILES: NetworkAccessProfile[] = [
  'onboarding',
  'full',
  'restricted',
  'quarantine',
  'deny',
]
interface NASBindingValues {
  configureEAP: boolean
  eapServerTrust?: NetworkEAPServerTrust
  accessMedium: NetworkAccessMedium
  coaSupported: boolean
  deviceType: NetworkAccessDeviceType
  disconnectSupported: boolean
  managementAddress?: string
  name: string
  nasId: string
  runtimeId: string
  siteId: string
  ssid?: string
  status: NetworkNASBindingStatus
}
interface NetworkNASBindingsPaneProps {
  canManage: boolean
  view?: 'network-device' | 'ssid' | 'user-admission'
}
export function NetworkNASBindingsPane({
  canManage,
  view = 'network-device',
}: NetworkNASBindingsPaneProps) {
  const { t, localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const queryClient = useQueryClient()
  const query = useQuery(networkAccessQueries.nasBindings(LIST_FILTER))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const create = useOperationMutation(networkAccessMutations.nasBindings.create(queryClient), {
    label: ['创建 NAS绑定', 'Create NAS binding'],
    target: (v) => v.nasId,
  })
  const update = useOperationMutation(networkAccessMutations.nasBindings.update(queryClient), {
    label: ['保存 NAS绑定', 'Save NAS binding'],
    target: (v) => v.bindingId,
  })
  const remove = useOperationMutation(networkAccessMutations.nasBindings.remove(queryClient), {
    label: ['删除 NAS绑定', 'Delete NAS binding'],
    target: (v) => v,
  })
  const operationBlocked =
    create.blocked ||
    update.blocked ||
    remove.blocked ||
    (query.isEnabled !== false && queryAccessDenied(query))
  const [editing, setEditing] = useState<NetworkNASBinding | null | undefined>(undefined)
  const [form] = Form.useForm<NASBindingValues>()
  const selectedMedium = Form.useWatch('accessMedium', form)
  const configureEAP = Form.useWatch('configureEAP', form)
  const fixedMedium: NetworkAccessMedium | undefined = view === 'ssid' ? 'wifi' : undefined
  const items = fixedMedium
    ? visibleQueryData?.filter((item) => item.accessMedium === fixedMedium)
    : visibleQueryData
  const addLabel =
    view === 'user-admission'
      ? t('networkAccess.add.admission', '新增入网配置')
      : view === 'ssid'
        ? t('networkAccess.add.ssid', '新增 SSID')
        : t('networkAccess.add.networkDevice', '新增网络设备')
  const editLabel =
    view === 'user-admission'
      ? t('networkAccess.edit.admission', '编辑入网配置')
      : view === 'ssid'
        ? t('networkAccess.edit.ssid', '编辑 SSID')
        : t('networkAccess.edit.networkDevice', '编辑网络设备')
  const deleteLabel =
    view === 'user-admission'
      ? t('networkAccess.delete.admission', '删除入网配置')
      : view === 'ssid'
        ? t('networkAccess.delete.ssid', '删除 SSID')
        : t('networkAccess.delete.networkDevice', '删除网络设备')
  const confirmDeleteLabel =
    view === 'user-admission'
      ? t('networkAccess.confirmDelete.admission', '确认删除入网配置？')
      : view === 'ssid'
        ? t('networkAccess.confirmDelete.ssid', '确认删除 SSID？')
        : t('networkAccess.confirmDelete.networkDevice', '确认删除网络设备？')
  const searchPlaceholder =
    view === 'user-admission'
      ? t('networkAccess.search.admission', '搜索 Wi-Fi、有线、SSID、NAS、运行时或站点')
      : view === 'ssid'
        ? t('networkAccess.search.ssids', '搜索 SSID、无线设备、NAS、运行时或站点')
        : t('networkAccess.search.networkDevices', '搜索设备、SSID、NAS、运行时或站点')
  function openEditor(record: NetworkNASBinding | null) {
    create.reset()
    update.reset()
    form.resetFields()
    setEditing(record)
    form.setFieldsValue({
      configureEAP: Boolean(record?.eapServerTrust),
      eapServerTrust: record?.eapServerTrust ?? {
        serverNames: [],
        caCertificates: [''],
        wifiSecurity: 'wpa2-enterprise',
      },
    })
    form.setFieldsValue(
      record ?? {
        accessMedium: fixedMedium ?? 'wired',
        name: '',
        nasId: '',
        runtimeId: '',
        siteId: '',
        deviceType: fixedMedium === 'wifi' ? 'wireless_controller' : 'switch',
        status: 'active',
        coaSupported: false,
        disconnectSupported: false,
      },
    )
  }
  function closeEditor() {
    if (create.isPending || update.isPending) return
    setEditing(undefined)
    form.resetFields()
  }
  async function submit() {
    try {
      const values = await form.validateFields()
      const input: NetworkNASBindingInput = {
        name: values.name.trim(),
        nasId: values.nasId.trim(),
        runtimeId: values.runtimeId.trim(),
        siteId: values.siteId.trim(),
        accessMedium: fixedMedium ?? values.accessMedium,
        deviceType: values.deviceType,
        ...(selectedMedium === 'wifi' && values.ssid?.trim() ? { ssid: values.ssid.trim() } : {}),
        ...(values.managementAddress?.trim()
          ? { managementAddress: values.managementAddress.trim() }
          : {}),
        status: values.status,
        coaSupported: values.coaSupported,
        disconnectSupported: values.disconnectSupported,
        ...(values.configureEAP && values.eapServerTrust
          ? { eapServerTrust: values.eapServerTrust }
          : {}),
      }
      if (editing) await update.mutateAsync({ bindingId: editing.id, input })
      else await create.mutateAsync(input)
      closeEditor()
    } catch {
      /* Retain the form and show the API failure below. */
    }
  }
  const columns: TableColumnsType<NetworkNASBinding> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 180 },
    {
      title: t('networkAccess.accessMedium', '入网方式'),
      dataIndex: 'accessMedium',
      width: 110,
      render: (value?: NetworkAccessMedium) =>
        value === 'wifi' ? 'Wi-Fi' : value === 'wired' ? t('networkAccess.wired', '有线') : '-',
    },
    {
      title: t('networkAccess.deviceType', '设备类型'),
      dataIndex: 'deviceType',
      width: 130,
      render: (value?: NetworkAccessDeviceType) => {
        if (!value) return '-'
        const labels: Record<NetworkAccessDeviceType, string> = {
          wireless_controller: t('networkAccess.deviceType.controller', '无线控制器'),
          access_point: t('networkAccess.deviceType.ap', '无线 AP'),
          switch: t('networkAccess.deviceType.switch', '交换机'),
          other: t('networkAccess.deviceType.other', '其他'),
        }
        return labels[value]
      },
    },
    { title: 'SSID', dataIndex: 'ssid', width: 150, render: (value?: string) => value || '-' },
    {
      title: t('networkAccess.managementAddress', '管理地址'),
      dataIndex: 'managementAddress',
      width: 170,
      render: (value?: string) => value || '-',
    },
    { title: 'NAS ID', dataIndex: 'nasId', width: 180 },
    { title: t('networkAccess.runtimeId', '运行时 ID'), dataIndex: 'runtimeId', width: 180 },
    { title: t('networkAccess.siteId', '站点 ID'), dataIndex: 'siteId', width: 180 },
    {
      title: 'CoA',
      dataIndex: 'coaSupported',
      width: 90,
      render: (value: boolean) => <BooleanTag value={value} />,
    },
    {
      title: t('networkAccess.disconnect', '断线'),
      dataIndex: 'disconnectSupported',
      width: 90,
      render: (value: boolean) => <BooleanTag value={value} />,
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    ...(canManage
      ? [
          {
            key: 'actions',
            width: 96,
            render: (_: unknown, record: NetworkNASBinding) => (
              <Space className="soha-row-action-icons">
                <ManagementIconButton
                  aria-label={editLabel}
                  icon={<EditOutlined />}
                  tooltip={t('common.edit', '编辑')}
                  onClick={() => openEditor(record)}
                  disabled={operationBlocked}
                />
                <Popconfirm
                  title={confirmDeleteLabel}
                  onConfirm={() => remove.mutate(record.id)}
                  okButtonProps={{ disabled: operationBlocked }}
                  disabled={operationBlocked}
                >
                  <ManagementIconButton
                    aria-label={deleteLabel}
                    danger
                    icon={<DeleteOutlined />}
                    tooltip={t('common.delete', '删除')}
                  />
                </Popconfirm>
              </Space>
            ),
          },
        ]
      : []),
  ]
  return (
    <>
      {!(editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))) && (
        <>
          {' '}
          {create.feedback}
          {update.feedback}
          {remove.feedback}{' '}
        </>
      )}

      <TablePane
        columns={columns}
        items={items}
        loading={query.isLoading}
        refreshing={query.isFetching}
        error={query.isEnabled === false ? null : query.error}
        onRefresh={() => void (query.isEnabled === false ? undefined : query.refetch())}
        searchPlaceholder={searchPlaceholder}
        getSearchValues={(item) => [
          item.name,
          item.nasId,
          item.runtimeId,
          item.siteId,
          item.accessMedium,
          item.deviceType,
          item.ssid,
          item.managementAddress,
          item.status,
        ]}
        createAction={
          canManage ? (
            <Button
              size="small"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => openEditor(null)}
              disabled={operationBlocked}
            >
              {addLabel}
            </Button>
          ) : null
        }
      />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        open={editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))}
        title={editing ? editLabel : addLabel}
        confirmLoading={create.isPending || update.isPending}
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
        {create.feedback}
        {update.feedback}
        {remove.feedback}

        <Form form={form} layout="vertical" disabled={operationBlocked}>
          {[
            ['name', t('networkAccess.name', '名称')],
            ['nasId', 'NAS ID'],
            ['runtimeId', t('networkAccess.runtimeId', '运行时 ID')],
            ['siteId', t('networkAccess.siteId', '站点 ID')],
          ].map(([name, label]) => (
            <Form.Item
              key={name}
              name={name}
              label={label}
              rules={[
                {
                  required: true,
                  whitespace: true,
                  message: t('networkAccess.form.required', '必填项'),
                },
              ]}
            >
              <Input maxLength={name === 'name' ? 200 : 128} />
            </Form.Item>
          ))}
          <Form.Item
            name="accessMedium"
            label={t('networkAccess.accessMedium', '入网方式')}
            rules={[{ required: true }]}
          >
            <Select
              disabled={operationBlocked || Boolean(fixedMedium)}
              options={[
                { value: 'wifi', label: 'Wi-Fi' },
                { value: 'wired', label: t('networkAccess.wired', '有线') },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="deviceType"
            label={t('networkAccess.deviceType', '设备类型')}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                {
                  value: 'wireless_controller',
                  label: t('networkAccess.deviceType.controller', '无线控制器'),
                },
                { value: 'access_point', label: t('networkAccess.deviceType.ap', '无线 AP') },
                { value: 'switch', label: t('networkAccess.deviceType.switch', '交换机') },
                { value: 'other', label: t('networkAccess.deviceType.other', '其他') },
              ]}
            />
          </Form.Item>
          {selectedMedium === 'wifi' ? (
            <Form.Item name="ssid" label="SSID">
              <Input maxLength={32} />
            </Form.Item>
          ) : null}
          <Form.Item
            name="managementAddress"
            label={t('networkAccess.managementAddress', '管理地址')}
            extra={t(
              'networkAccess.managementAddressHint',
              '填写交换机、AP 或无线控制器的管理 IP/主机名，不保存设备密码。',
            )}
          >
            <Input maxLength={255} placeholder="10.0.10.2" />
          </Form.Item>
          <Form.Item
            name="status"
            label={t('networkAccess.status', '状态')}
            rules={[{ required: true }]}
          >
            <Select options={['active', 'disabled'].map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item name="coaSupported" label="CoA" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            name="disconnectSupported"
            label={t('networkAccess.disconnectSupported', '支持断线重认证')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Divider titlePlacement="start">EAP-TLS</Divider>
          <Form.Item
            name="configureEAP"
            valuePropName="checked"
            label={text('配置终端的认证服务器校验', 'Configure authentication server validation')}
            extra={text(
              '只填写公开 CA 证书和精确 DNS 名称。关闭会清除此项；终端不得跳过服务器证书校验。',
              'Provide public CA certificates and exact DNS names. Turning this off clears the setting; endpoints must retain server certificate validation.',
            )}
          >
            <Switch />
          </Form.Item>
          {configureEAP && (
            <>
              <Form.Item
                name={['eapServerTrust', 'serverNames']}
                label={text('认证服务器 DNS 名称', 'Authentication server DNS names')}
                rules={[{ required: true, type: 'array', min: 1, max: 16 }]}
              >
                <Select
                  mode="tags"
                  maxCount={16}
                  tokenSeparators={[',', ' ']}
                  placeholder="radius.example.com"
                />
              </Form.Item>
              <Form.Item
                name={['eapServerTrust', 'wifiSecurity']}
                label={text('无线安全协议', 'Wi-Fi security protocol')}
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { value: 'wpa2-enterprise', label: 'WPA2-Enterprise' },
                    { value: 'wpa3-enterprise', label: 'WPA3-Enterprise' },
                  ]}
                />
              </Form.Item>
              <Form.List
                name={['eapServerTrust', 'caCertificates']}
                rules={[
                  {
                    validator: async (_, values) => {
                      if (!values?.length || values.length > 8)
                        throw new Error(
                          text(
                            '至少一个、最多八个 CA 证书',
                            'Provide one to eight CA certificates',
                          ),
                        )
                    },
                  },
                ]}
              >
                {(fields, { add, remove }, { errors }) => (
                  <>
                    {fields.map((field) => (
                      <div key={field.key}>
                        <Form.Item
                          name={field.name}
                          label={text('公开 CA 证书（PEM）', 'Public CA certificate (PEM)')}
                          rules={[{ required: true, whitespace: true, max: 16384 }]}
                        >
                          <Input.TextArea autoSize={{ minRows: 3, maxRows: 8 }} maxLength={16384} />
                        </Form.Item>
                        {fields.length > 1 && (
                          <Button size="small" onClick={() => remove(field.name)}>
                            {text('移除此 CA', 'Remove this CA')}
                          </Button>
                        )}
                      </div>
                    ))}
                    <Button size="small" disabled={fields.length >= 8} onClick={() => add('')}>
                      {text('添加 CA 证书', 'Add CA certificate')}
                    </Button>
                    <Form.ErrorList errors={errors} />
                  </>
                )}
              </Form.List>
            </>
          )}
          {(create.error || update.error) && (
            <Alert
              showIcon
              type="error"
              title={text('配置未保存', 'Configuration was not saved')}
              description={(create.error ?? update.error)?.message}
            />
          )}
        </Form>
      </Modal>
    </>
  )
}
interface SiteProfileValues {
  accessProfile: NetworkAccessProfile
  filterId?: string
  sessionTimeoutSeconds: number
  siteId: string
  vlanId?: number
}
export function NetworkSiteProfileBindingsPane({ canManage }: { canManage: boolean }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const query = useQuery(networkAccessQueries.siteProfileBindings(LIST_FILTER))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const create = useOperationMutation(
    networkAccessMutations.siteProfileBindings.create(queryClient),
    { label: ['创建 站点绑定', 'Create site binding'], target: (v) => v.siteId },
  )
  const update = useOperationMutation(
    networkAccessMutations.siteProfileBindings.update(queryClient),
    { label: ['保存 站点绑定', 'Save site binding'], target: (v) => v.bindingId },
  )
  const remove = useOperationMutation(
    networkAccessMutations.siteProfileBindings.remove(queryClient),
    { label: ['删除 站点绑定', 'Delete site binding'], target: (v) => v },
  )
  const operationBlocked =
    create.blocked ||
    update.blocked ||
    remove.blocked ||
    (query.isEnabled !== false && queryAccessDenied(query))
  const [editing, setEditing] = useState<NetworkSiteProfileBinding | null | undefined>(undefined)
  const [form] = Form.useForm<SiteProfileValues>()
  function openEditor(record: NetworkSiteProfileBinding | null) {
    setEditing(record)
    form.setFieldsValue(
      record ?? { siteId: '', accessProfile: 'restricted', sessionTimeoutSeconds: 900 },
    )
  }
  function closeEditor() {
    setEditing(undefined)
    form.resetFields()
  }
  async function submit() {
    const values = await form.validateFields()
    const input: NetworkSiteProfileBindingInput = {
      siteId: values.siteId.trim(),
      accessProfile: values.accessProfile,
      sessionTimeoutSeconds: values.sessionTimeoutSeconds,
      ...(values.vlanId ? { vlanId: values.vlanId } : {}),
      ...(values.filterId?.trim() ? { filterId: values.filterId.trim() } : {}),
    }
    if (editing) await update.mutateAsync({ bindingId: editing.id, input })
    else await create.mutateAsync(input)
    closeEditor()
  }
  const columns: TableColumnsType<NetworkSiteProfileBinding> = [
    { title: t('networkAccess.siteId', '站点 ID'), dataIndex: 'siteId', width: 200 },
    {
      title: t('networkAccess.accessProfile', '接入等级'),
      dataIndex: 'accessProfile',
      width: 140,
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: 'VLAN', dataIndex: 'vlanId', width: 90, render: (value?: number) => value ?? '-' },
    {
      title: 'Filter-Id',
      dataIndex: 'filterId',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: t('networkAccess.sessionTimeout', '会话时长（秒）'),
      dataIndex: 'sessionTimeoutSeconds',
      width: 150,
    },
    ...(canManage
      ? [
          {
            key: 'actions',
            width: 96,
            render: (_: unknown, record: NetworkSiteProfileBinding) => (
              <Space className="soha-row-action-icons">
                <ManagementIconButton
                  aria-label={t('networkAccess.edit.siteProfile', '编辑接入等级')}
                  icon={<EditOutlined />}
                  tooltip={t('common.edit', '编辑')}
                  onClick={() => openEditor(record)}
                  disabled={operationBlocked}
                />
                <Popconfirm
                  title={t('networkAccess.confirmDelete.siteProfile', '确认删除接入等级？')}
                  onConfirm={() => remove.mutate(record.id)}
                  okButtonProps={{ disabled: operationBlocked }}
                  disabled={operationBlocked}
                >
                  <ManagementIconButton
                    aria-label={t('networkAccess.delete.siteProfile', '删除接入等级')}
                    danger
                    icon={<DeleteOutlined />}
                    tooltip={t('common.delete', '删除')}
                  />
                </Popconfirm>
              </Space>
            ),
          },
        ]
      : []),
  ]
  return (
    <>
      {!(editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))) && (
        <>
          {' '}
          {create.feedback}
          {update.feedback}
          {remove.feedback}{' '}
        </>
      )}

      <TablePane
        columns={columns}
        items={visibleQueryData}
        loading={query.isLoading}
        refreshing={query.isFetching}
        error={query.isEnabled === false ? null : query.error}
        onRefresh={() => void (query.isEnabled === false ? undefined : query.refetch())}
        searchPlaceholder={t('networkAccess.search.siteProfiles', '搜索站点、等级或 Filter-Id')}
        getSearchValues={(item) => [item.siteId, item.accessProfile, item.filterId, item.vlanId]}
        createAction={
          canManage ? (
            <Button
              size="small"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => openEditor(null)}
              disabled={operationBlocked}
            >
              {t('networkAccess.add.siteProfile', '新增接入等级')}
            </Button>
          ) : null
        }
      />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        open={editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))}
        title={
          editing
            ? t('networkAccess.edit.siteProfile', '编辑接入等级')
            : t('networkAccess.add.siteProfile', '新增接入等级')
        }
        confirmLoading={create.isPending || update.isPending}
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
        {create.feedback}
        {update.feedback}
        {remove.feedback}

        <Form form={form} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="siteId"
            label={t('networkAccess.siteId', '站点 ID')}
            rules={[
              {
                required: true,
                whitespace: true,
                message: t('networkAccess.form.required', '必填项'),
              },
            ]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="accessProfile"
            label={t('networkAccess.accessProfile', '接入等级')}
            rules={[{ required: true }]}
          >
            <Select options={ACCESS_PROFILES.map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item name="vlanId" label="VLAN ID">
            <InputNumber min={1} max={4094} precision={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="filterId" label="Filter-Id">
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="sessionTimeoutSeconds"
            label={t('networkAccess.sessionTimeout', '会话时长（秒）')}
            rules={[{ required: true }]}
          >
            <InputNumber min={1} max={86400} precision={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
interface SessionActionValues {
  action: NetworkSessionAction
  reasonCode: string
  targetAccessProfile: NetworkAccessProfile
}
export function NetworkSessionsPane({ canManage }: { canManage: boolean }) {
  const { t, localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const queryClient = useQueryClient()
  const query = useQuery(networkAccessQueries.sessions(LIST_FILTER))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const planAction = useOperationMutation(networkAccessMutations.sessions.plan(), {
    label: ['会话操作预检', 'Plan session action session'],
    target: (v) => v.sessionId,
    readOnly: true,
  })
  const executeAction = useOperationMutation(networkAccessMutations.sessions.execute(queryClient), {
    label: ['执行会话命令', 'Execute session command session'],
    target: (v) => v.sessionId,
    receipt: (command) => ({
      message: '会话命令已返回：' + command.status,
      evidence: '命令 ID：' + command.id + '；实际接入状态以会话记录为准。',
    }),
  })
  const operationBlocked =
    executeAction.blocked || (query.isEnabled !== false && queryAccessDenied(query))
  const [session, setSession] = useState<NetworkSession | null>(null)
  const [plan, setPlan] = useState<NetworkSessionActionPlan | null>(null)
  const [form] = Form.useForm<SessionActionValues>()
  function openAction(record: NetworkSession) {
    if (operationBlocked) return
    planAction.reset()
    executeAction.reset()
    setSession(record)
    setPlan(null)
    form.setFieldsValue({ action: 'coa', reasonCode: '', targetAccessProfile: undefined })
  }
  function closeAction() {
    if (planAction.isPending || executeAction.isPending) return
    setSession(null)
    setPlan(null)
    form.resetFields()
  }
  async function submitAction() {
    if (!session) return
    try {
      if (!plan) {
        const values = await form.validateFields()
        const next = await planAction.mutateAsync({
          sessionId: session.id,
          input: { ...values, reasonCode: values.reasonCode.trim() },
        })
        setPlan(next)
        return
      }
      await executeAction.mutateAsync({
        sessionId: session.id,
        input: {
          action: plan.requestedAction,
          targetAccessProfile: plan.targetAccessProfile,
          reasonCode: plan.reasonCode,
          planHash: plan.planHash,
        },
      })
      closeAction()
    } catch {
      /* Keep the reviewed plan and show the API reason. */
    }
  }
  const columns: TableColumnsType<NetworkSession> = [
    { title: t('networkAccess.subjectId', '用户 ID'), dataIndex: 'subjectId', width: 190 },
    { title: t('networkAccess.deviceId', '设备 ID'), dataIndex: 'deviceId', width: 190 },
    {
      title: t('networkAccess.siteId', '站点 ID'),
      dataIndex: 'siteId',
      width: 160,
      render: (value) => value || '-',
    },
    { title: 'NAS ID', dataIndex: 'nasId', width: 150, render: (value) => value || '-' },
    { title: t('networkAccess.mode', '接入模式'), dataIndex: 'mode', width: 150 },
    { title: t('networkAccess.pathMode', '路径'), dataIndex: 'path', width: 140 },
    {
      title: t('networkAccess.accessProfile', '接入等级'),
      dataIndex: 'accessProfile',
      width: 130,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: t('networkAccess.policyVersion', '策略版本'),
      dataIndex: 'policyVersion',
      width: 190,
      render: (value: number, row) => (
        <Space orientation="vertical" size={0}>
          <span>{value}</span>
          {row.admissionAttributeVersion && (
            <Typography.Text type="secondary" title={row.admissionAttributeVersion}>
              {text('入网版本', 'Admission version')} {row.admissionPolicyVersion} ·{' '}
              {row.admissionAttributeVersion.slice(7, 19)}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: text('持久限制', 'Persisted restriction'),
      dataIndex: 'restrictionProfile',
      width: 140,
      render: (value?: string) =>
        value ? <StatusTag value={value} label={responseLabel(value, localeCode)} /> : '—',
    },
    {
      title: text('NAS 执行回执', 'NAS command receipt'),
      key: 'command',
      width: 230,
      render: (_, row) =>
        row.latestCommand ? (
          <Space orientation="vertical" size={2}>
            <StatusTag
              value={
                row.latestCommand.status === 'applied' ? 'acknowledged' : row.latestCommand.status
              }
              label={
                row.latestCommand.status === 'applied'
                  ? text('NAS 已确认，待网络验证', 'NAS acknowledged; network verification pending')
                  : undefined
              }
            />
            <span>
              {responseLabel(row.latestCommand.action, localeCode)} ·{' '}
              {formatDateTime(row.latestCommand.expiresAt)}
            </span>
          </Space>
        ) : (
          '—'
        ),
    },
    ...(canManage
      ? [
          {
            key: 'actions',
            width: 64,
            render: (_: unknown, record: NetworkSession) => (
              <ManagementIconButton
                aria-label={t('networkAccess.sessionAction', '调整会话访问')}
                disabled={record.status !== 'active'}
                icon={<SafetyCertificateOutlined />}
                tooltip={t('networkAccess.sessionAction', '调整会话访问')}
                onClick={() => openAction(record)}
              />
            ),
          },
        ]
      : []),
  ]
  return (
    <>
      {!(Boolean(session) && !(query.isEnabled !== false && queryAccessDenied(query))) && (
        <>
          {' '}
          {planAction.feedback}
          {executeAction.feedback}{' '}
        </>
      )}

      <TablePane
        columns={columns}
        items={visibleQueryData}
        loading={query.isLoading}
        refreshing={query.isFetching}
        error={query.isEnabled === false ? null : query.error}
        onRefresh={() => void (query.isEnabled === false ? undefined : query.refetch())}
        searchPlaceholder={t('networkAccess.search.sessions', '搜索用户、设备、站点或 NAS')}
        getSearchValues={(item) => [
          item.subjectId,
          item.deviceId,
          item.siteId,
          item.nasId,
          item.mode,
          item.path,
          item.accessProfile,
          item.status,
        ]}
      />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        width={640}
        open={Boolean(session) && !(query.isEnabled !== false && queryAccessDenied(query))}
        title={t('networkAccess.sessionAction', '调整会话访问')}
        confirmLoading={planAction.isPending || executeAction.isPending}
        okButtonProps={{ ...{ danger: Boolean(plan?.willDisconnect) }, disabled: operationBlocked }}
        okText={
          plan
            ? t('networkAccess.sessionActionExecute', '确认执行')
            : t('networkAccess.sessionActionPlan', '生成计划')
        }
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) closeAction()
        }}
        onOk={() => void submitAction()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
      >
        {planAction.feedback}
        {executeAction.feedback}

        <Form form={form} layout="vertical" disabled={operationBlocked || Boolean(plan)}>
          <Form.Item
            name="action"
            label={t('networkAccess.requestedAction', '请求动作')}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: 'coa', label: 'CoA' },
                { value: 'disconnect', label: t('networkAccess.disconnect', '断线') },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="targetAccessProfile"
            label={t('networkAccess.targetAccessProfile', '目标接入等级')}
            rules={[
              { required: true },
              {
                validator: (_, value) =>
                  value && value !== session?.accessProfile
                    ? Promise.resolve()
                    : Promise.reject(
                        new Error(t('networkAccess.sessionActionNoop', '目标接入等级必须发生变化')),
                      ),
              },
            ]}
          >
            <Select options={ACCESS_PROFILES.map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item
            name="reasonCode"
            label={t('networkAccess.reasonCode', '原因代码')}
            rules={[
              {
                required: true,
                whitespace: true,
                message: t('networkAccess.form.required', '必填项'),
              },
            ]}
          >
            <Input maxLength={128} placeholder="risk_change" />
          </Form.Item>
        </Form>
        {plan ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Button
              disabled={executeAction.isPending}
              onClick={() => {
                setPlan(null)
                planAction.reset()
                executeAction.reset()
              }}
            >
              {text('重新生成计划', 'Regenerate plan')}
            </Button>
            {plan.willDisconnect ? (
              <Alert
                showIcon
                type="warning"
                title={t(
                  'networkAccess.sessionWillDisconnect',
                  '该操作会断开当前会话并要求重新认证',
                )}
                description={
                  plan.effectiveAction !== plan.requestedAction
                    ? t('networkAccess.coaFallback', 'NAS 不支持 CoA，已安全降级为断线重认证。')
                    : undefined
                }
              />
            ) : null}
            <Descriptions
              bordered
              size="small"
              column={2}
              items={[
                {
                  key: 'requested',
                  label: t('networkAccess.requestedAction', '请求动作'),
                  children: plan.requestedAction,
                },
                {
                  key: 'effective',
                  label: t('networkAccess.effectiveAction', '实际动作'),
                  children: plan.effectiveAction,
                },
                {
                  key: 'current',
                  label: t('networkAccess.currentAccessProfile', '当前等级'),
                  children: plan.currentAccessProfile,
                },
                {
                  key: 'target',
                  label: t('networkAccess.targetAccessProfile', '目标接入等级'),
                  children: plan.targetAccessProfile,
                },
                {
                  key: 'expires',
                  label: t('networkAccess.commandExpiresAt', '命令过期时间'),
                  children: plan.commandExpiresAt,
                  span: 2,
                },
              ]}
            />
          </Space>
        ) : null}
      </Modal>
    </>
  )
}
