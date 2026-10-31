import { AdminTable } from '@/components/admin-table'
import {
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
} from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { ApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { PlusOutlined } from '@ant-design/icons'
import type {
  NetworkProxyConfigurationPatchInput,
  NetworkProxyEngine,
  NetworkProxyInstance,
  NetworkProxyInstanceInput,
  NetworkRuntimeEnrollmentSecret,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, App, Button, Form, Input, Modal, Select, Space, Switch, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { MetadataTag } from '@/components/status-tag'
import { NetworkStatusTag } from '../network-status-tag'
import '../styles.css'
import { networkAccessKeys } from '../keys'
import { createEnrollment, createInstance, updateConfiguration } from './api'
import { proxyKeys, proxyQueries } from './queries'
import './styles.css'
import { ProxyConfigurationHistory } from './history'
const engines: NetworkProxyEngine[] = ['mihomo', 'sing-box', 'v2ray']
export default function ProxyInstancesPage() {
  const { t, localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissions = usePermissionSnapshot()
  const can = (key: string) => hasPermission(permissions.data?.data, key)
  const canView = can('network_access.proxy_instances.view')
  const canCreate = can('network_access.proxy_instances.create')
  const canUpdate = can('network_access.proxy_instances.update')
  const canEnroll = can('network_access.enrollments.create')
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [draftEngine, setDraftEngine] = useState<NetworkProxyEngine | undefined>()
  const [engine, setEngine] = useState<NetworkProxyEngine | undefined>()
  const instances = useQuery(proxyQueries.instances({ search: keyword, engine }, canView))
  const visibleInstancesData =
    instances.isEnabled === false ? undefined : readableQueryData(instances)
  const [createOpen, setCreateOpen] = useState(false)
  const [configurationTarget, setConfigurationTarget] = useState<NetworkProxyInstance>()
  const [secret, setSecret] = useState<NetworkRuntimeEnrollmentSecret>()
  const [createForm] = Form.useForm<NetworkProxyInstanceInput>()
  const [configurationForm] = Form.useForm<NetworkProxyConfigurationPatchInput>()
  const readDenied = instances.isEnabled !== false && queryAccessDenied(instances)
  useEffect(() => {
    if (!canUpdate || readDenied) {
      setConfigurationTarget(undefined)
      configurationForm.resetFields()
    }
    if (!canCreate || readDenied) {
      setCreateOpen(false)
      createForm.resetFields()
    }
    if (!canEnroll || readDenied) setSecret(undefined)
  }, [canUpdate, canCreate, canEnroll, readDenied, configurationForm, createForm])
  const configurationEnabled = Form.useWatch('enabled', configurationForm)
  const clearingBinding = Form.useWatch('clearBinding', configurationForm)
  const refresh = () => void queryClient.invalidateQueries({ queryKey: proxyKeys.all })
  const create = useOperationMutation(
    {
      mutationFn: createInstance,
      onSuccess: () => {
        setCreateOpen(false)
        createForm.resetFields()
        refresh()
      },
    },
    { label: ['创建 代理实例', 'Create proxy instance'], target: (v) => v.name },
  )
  const configure = useOperationMutation(
    {
      mutationFn: ({ id, input }: { id: string; input: NetworkProxyConfigurationPatchInput }) =>
        updateConfiguration(id, input),
      onSuccess: () => {
        setConfigurationTarget(undefined)
        configurationForm.resetFields()
        refresh()
      },
      onError: (error) => {
        if (error instanceof ApiError && error.status === 409)
          message.info('配置版本冲突，请关闭并重新打开配置，加载最新版本后提交。')
        refresh()
      },
    },
    {
      label: ['保存 代理实例', 'Save proxy instance'],
      target: (v) => v.id,
      receipt: (data) => ({
        message: '期望配置已保存，等待运行时应用',
        evidence: `期望版本 r${data.desiredRevision} / 观测版本 r${data.observedRevision}；以运行时观测结果为准。`,
      }),
    },
  )
  const enroll = useOperationMutation(
    {
      mutationFn: createEnrollment,
      onSuccess: (value) => {
        setSecret(value)
        void queryClient.invalidateQueries({ queryKey: networkAccessKeys.enrollments.all })
      },
    },
    { label: ['创建 代理实例', 'Create proxy instance'], target: (v) => v },
  )
  const operationBlocked =
    create.blocked ||
    configure.blocked ||
    enroll.blocked ||
    (instances.isEnabled !== false && queryAccessDenied(instances))
  const columns: TableColumnsType<NetworkProxyInstance> = [
    { title: t('networkAccess.proxyRuntime.name', '名称'), dataIndex: 'name', ellipsis: true },
    { title: t('networkAccess.proxyRuntime.id', '实例 ID'), dataIndex: 'id', ellipsis: true },
    {
      title: t('networkAccess.proxyRuntime.engine', '引擎'),
      dataIndex: 'engine',
      width: 105,
      render: (value: string) => <MetadataTag label={value} />,
    },
    {
      title: t('networkAccess.proxyRuntime.status', '状态'),
      dataIndex: 'status',
      width: 100,
      render: (status: NetworkProxyInstance['status'], item) => (
        <Space orientation="vertical" size={4}>
          <NetworkStatusTag value={status} />
          {item.securityRestricted ? (
            <MetadataTag label={text('DLP 限制', 'DLP restricted')} />
          ) : null}
        </Space>
      ),
    },
    {
      title: t('networkAccess.proxyRuntime.version', '版本'),
      dataIndex: 'engineVersion',
      width: 120,
      render: (value?: string) => value || '—',
    },
    {
      title: text('已应用 / 期望配置', 'Applied / desired configuration'),
      width: 115,
      render: (_, item) => `${item.observedRevision || '—'} / ${item.desiredRevision || '—'}`,
    },
    {
      title: text('站点 / 宿主设备', 'Site / host device'),
      width: 180,
      render: (_, item) =>
        item.binding
          ? `${item.binding.siteId} / ${item.binding.deviceId}`
          : text('未绑定', 'Unbound'),
    },
    {
      title: text('已应用 / 期望策略', 'Applied / desired policy'),
      width: 115,
      render: (_, item) => `${item.observedPolicyVersion || '—'} / ${item.policyVersion || '—'}`,
    },
    {
      title: text('运行时应用证据', 'Runtime application evidence'),
      width: 160,
      render: (_, item) =>
        item.status !== 'online' ||
        item.observedRevision !== item.desiredRevision ||
        !item.observedPolicyVersion
          ? text('待确认', 'Pending')
          : item.capabilities.includes('protection_rules_readback')
            ? text('引擎规则回读', 'Engine rule readback')
            : item.capabilities.includes('protected_destinations_v1')
              ? text('文件与进程检查', 'File and process checks')
              : text('无保护证据', 'No protection evidence'),
    },
    {
      title: text('最近声明能力', 'Last declared capabilities'),
      dataIndex: 'capabilities',
      width: 210,
      render: (values: string[]) =>
        values.length ? values.join(', ') : text('未声明', 'Not declared'),
    },
    {
      title: text('诊断原因', 'Diagnostic reason'),
      dataIndex: 'reasonCode',
      ellipsis: true,
      render: (value?: string) => value || '—',
    },
    {
      title: t('networkAccess.proxyRuntime.lastSeen', '最近联系'),
      dataIndex: 'lastSeenAt',
      width: 175,
      render: (value?: string) => (value ? formatDateTime(value) : '—'),
    },
    {
      title: t('common.actions', '操作'),
      key: 'actions',
      className: 'soha-table-actions-column',
      width: 245,
      fixed: 'right',
      render: (_, item) => (
        <Space size={4}>
          <ProxyConfigurationHistory
            instance={item}
            canUpdate={canUpdate}
            disabled={operationBlocked}
          />
          {canUpdate ? (
            <Button
              size="small"
              onClick={() => {
                setConfigurationTarget(item)
                configurationForm.setFieldsValue({
                  expectedRevision: item.desiredRevision,
                  enabled: item.enabled,
                  content: '',
                  binding: item.binding,
                  clearBinding: false,
                })
              }}
            >
              {t('networkAccess.proxyRuntime.configure', '配置')}
            </Button>
          ) : null}
          {canEnroll ? (
            <Button
              size="small"
              loading={enroll.isPending && enroll.variables === item.id}
              onClick={() => enroll.mutate(item.id)}
              disabled={operationBlocked}
            >
              {t('networkAccess.proxyRuntime.enroll', '登记令牌')}
            </Button>
          ) : null}
        </Space>
      ),
    },
  ]
  if (!canView)
    return <ManagementState kind={permissions.isLoading ? 'loading' : 'no-permission'} />
  return (
    <main className="soha-server-proxy-page">
      {!(
        (createOpen && !(instances.isEnabled !== false && queryAccessDenied(instances))) ||
        (canUpdate && Boolean(configurationTarget) && !readDenied) ||
        (Boolean(secret) && !(instances.isEnabled !== false && queryAccessDenied(instances)))
      ) && (
        <>
          {' '}
          {create.feedback}
          {configure.feedback}
          {enroll.feedback}{' '}
        </>
      )}

      <h1 className="soha-network-sr-only">
        {t('networkAccess.proxyRuntime.instances', '代理实例')}
      </h1>
      <Typography.Text type="secondary">
        {t(
          'networkAccess.proxyRuntime.instanceScope',
          '管理服务器上的 Mihomo、sing-box 与 V2Ray 实例。配置保存后由已登记的 Linux 运行时拉取并应用。',
        )}
      </Typography.Text>
      <Alert
        type="info"
        showIcon
        title={text(
          '受管代理仅覆盖经过本实例的 TCP 流量；UDP、TUN、非代理应用不在此拦截范围。受保护资源须由网关 ACL 和 VPN/ZTNA 控制，系统代理开关不能替代网络隔离。',
          'Managed proxy enforcement covers TCP passing through this instance. UDP, TUN and applications bypassing the proxy need gateway ACL and VPN/ZTNA enforcement. A system proxy setting does not provide network isolation.',
        )}
      />
      <ManagementQueryPanel
        onFinish={() => {
          setKeyword(draft.trim())
          setEngine(draftEngine)
        }}
        actions={
          <ManagementQueryActions
            disabledReset={!draft && !keyword && !draftEngine && !engine}
            onReset={() => {
              setDraft('')
              setKeyword('')
              setDraftEngine(undefined)
              setEngine(undefined)
            }}
          />
        }
      >
        <ManagementKeywordField
          value={draft}
          onChange={setDraft}
          placeholder={t('networkAccess.proxyRuntime.searchInstances', '搜索名称、实例 ID 或主机')}
        />
        <ManagementQueryField label={t('networkAccess.proxyRuntime.engine', '引擎')} width={220}>
          <Select
            aria-label={t('networkAccess.proxyRuntime.engine', '引擎')}
            allowClear
            placeholder={t('networkAccess.proxyRuntime.allEngines', '全部引擎')}
            value={draftEngine}
            onChange={setDraftEngine}
            options={engines.map((value) => ({ value, label: value }))}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      {visibleInstancesData?.length === 200 ? (
        <Typography.Text type="secondary">
          {t(
            'networkAccess.proxyRuntime.resultLimit',
            '最多展示 200 个匹配实例。可使用上方条件缩小查询范围。',
          )}
        </Typography.Text>
      ) : null}
      <AdminTable
        columns={columns}
        scroll={{ x: 1640 }}
        dataSource={visibleInstancesData ?? []}
        rowKey="id"
        loading={instances.isLoading}
        refreshing={instances.isFetching}
        error={instances.error ?? undefined}
        hasReadData={visibleInstancesData !== undefined}
        onRefresh={() => void (instances.isEnabled === false ? undefined : instances.refetch())}
        localSorting
        pagination={{ pageSize: 15, showSizeChanger: false }}
        toolbarExtra={
          canCreate ? (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setCreateOpen(true)}
              disabled={operationBlocked}
            >
              {t('networkAccess.proxyRuntime.add', '新增实例')}
            </Button>
          ) : null
        }
        empty={t('networkAccess.proxyRuntime.noInstances', '尚无代理实例。请先创建并登记运行时。')}
      />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        title={t('networkAccess.proxyRuntime.add', '新增实例')}
        open={canCreate && createOpen && !readDenied}
        destroyOnHidden
        confirmLoading={create.isPending}
        onCancel={() => {
          if (!operationBlocked) (() => setCreateOpen(false))()
        }}
        onOk={() => {
          if (canCreate && !readDenied)
            void createForm.validateFields().then((values) => create.mutate(values))
        }}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {create.feedback}
        {configure.feedback}
        {enroll.feedback}

        <Form form={createForm} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="id"
            label={t('networkAccess.proxyRuntime.id', '实例 ID')}
            rules={[{ required: true, pattern: /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/ }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="name"
            label={t('networkAccess.proxyRuntime.name', '名称')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="engine"
            label={t('networkAccess.proxyRuntime.engine', '引擎')}
            rules={[{ required: true }]}
          >
            <Select options={engines.map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item name="host" label={t('networkAccess.proxyRuntime.host', '主机标识')}>
            <Input maxLength={128} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        title={
          configurationTarget
            ? `${t('networkAccess.proxyRuntime.configure', '配置')} · ${configurationTarget.name}`
            : ''
        }
        open={canUpdate && Boolean(configurationTarget) && !readDenied}
        destroyOnHidden
        width={760}
        confirmLoading={configure.isPending}
        onCancel={() => {
          if (!operationBlocked) (() => setConfigurationTarget(undefined))()
        }}
        onOk={() => {
          if (!configurationTarget || !canUpdate || readDenied) return
          void configurationForm.validateFields().then((input) => {
            if (!input.content?.trim()) delete input.content
            if (input.clearBinding) delete input.binding
            if (
              input.binding &&
              !input.binding.siteId &&
              !input.binding.deviceId &&
              !input.binding.resourceIds?.length
            )
              delete input.binding
            const previous = configurationTarget.binding
            if (
              input.binding &&
              previous &&
              input.binding.siteId === previous.siteId &&
              input.binding.deviceId === previous.deviceId &&
              JSON.stringify([...input.binding.resourceIds].sort()) ===
                JSON.stringify([...previous.resourceIds].sort())
            )
              delete input.binding
            configure.mutate({ id: configurationTarget.id, input })
          })
        }}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {create.feedback}
        {configure.feedback}
        {enroll.feedback}

        <Alert
          type="info"
          showIcon
          title={t(
            'networkAccess.proxyRuntime.contentWriteOnly',
            '配置内容只写且不回显。首次提交完整配置；之后留空保留原内容，绑定和停用操作无需重新粘贴。',
          )}
        />
        <Form
          form={configurationForm}
          layout="vertical"
          className="soha-server-proxy-config-form"
          disabled={operationBlocked || !canUpdate}
        >
          <Alert
            type="info"
            showIcon
            title={text(
              '绑定只标识管理范围，不授予资源访问权。三引擎将优先拒绝已发布的受保护目标，资源访问请使用 VPN/ZTNA。',
              'Bindings identify administrative scope and do not grant access. All three engines reject published protected destinations before user rules; use VPN/ZTNA for resource access.',
            )}
          />
          <Form.Item
            name="clearBinding"
            label={text('停用并解除绑定', 'Disable and remove binding')}
            valuePropName="checked"
          >
            <Switch
              onChange={(value) => {
                if (value) configurationForm.setFieldValue('enabled', false)
              }}
            />
          </Form.Item>
          <Form.Item
            name={['binding', 'siteId']}
            label={text('站点 ID', 'Site ID')}
            rules={
              clearingBinding
                ? []
                : [
                    {
                      required: Boolean(configurationEnabled || configurationTarget?.binding),
                      pattern: /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/,
                    },
                  ]
            }
          >
            <Input maxLength={128} disabled={clearingBinding} />
          </Form.Item>
          <Form.Item
            name={['binding', 'deviceId']}
            label={text('宿主设备 ID', 'Host device ID')}
            rules={
              clearingBinding
                ? []
                : [
                    {
                      required: Boolean(configurationEnabled || configurationTarget?.binding),
                      pattern: /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/,
                    },
                  ]
            }
          >
            <Input maxLength={128} disabled={clearingBinding} />
          </Form.Item>
          <Form.Item
            name={['binding', 'resourceIds']}
            initialValue={[]}
            label={text('关联受保护资源 ID', 'Related protected resource IDs')}
            extra={text(
              '资源必须属于该站点；此列表不会缩小全局保护清单。绑定需具备关联对象的查看权限。',
              'Resources must belong to this site. This list does not narrow the global protection set. Binding requires permission to view the related objects.',
            )}
            rules={
              clearingBinding
                ? []
                : [
                    {
                      validator: (_, value: string[] | undefined) =>
                        !value ||
                        (value.length <= 128 &&
                          value.every((id) => /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id)))
                          ? Promise.resolve()
                          : Promise.reject(
                              new Error(
                                text(
                                  '最多填写 128 个合法资源 ID',
                                  'Enter at most 128 valid resource IDs',
                                ),
                              ),
                            ),
                    },
                  ]
            }
          >
            <Select mode="tags" tokenSeparators={[',', ' ', '\n']} disabled={clearingBinding} />
          </Form.Item>
          <Form.Item name="expectedRevision" hidden>
            <Input />
          </Form.Item>
          <Form.Item
            name="enabled"
            label={t('networkAccess.proxyRuntime.enabled', '启用引擎')}
            valuePropName="checked"
          >
            <Switch disabled={clearingBinding} />
          </Form.Item>
          <Form.Item
            name="content"
            label={t('networkAccess.proxyRuntime.content', '完整配置')}
            rules={[{ required: configurationTarget?.desiredRevision === 0, whitespace: true }]}
            extra={text(
              '首次必须提交完整配置；之后留空保留已加密保存的内容。应用回执不等于真实流量阻断证明。',
              'Provide the full configuration initially; leave blank later to retain the encrypted content. Application receipts do not establish data-plane blocking.',
            )}
          >
            <Input.TextArea rows={14} maxLength={1048576} spellCheck={false} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        title={t('networkAccess.proxyRuntime.enrollmentSecret', '一次性登记信息')}
        open={Boolean(secret) && !(instances.isEnabled !== false && queryAccessDenied(instances))}
        destroyOnHidden
        cancelButtonProps={{ ...{ style: { display: 'none' } }, disabled: operationBlocked }}
        okText={t('networkAccess.proxyRuntime.saved', '我已保存')}
        onCancel={() => {
          if (!operationBlocked) (() => setSecret(undefined))()
        }}
        onOk={() => setSecret(undefined)}
        closable={!operationBlocked}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {create.feedback}
        {configure.feedback}
        {enroll.feedback}

        <Alert
          type="warning"
          showIcon
          title={t(
            'networkAccess.proxyRuntime.secretOnce',
            '令牌只显示一次，请保存到运行时私有文件。有效期 10 分钟。',
          )}
        />
        <div className="soha-server-proxy-secret">
          <Typography.Text>Enrollment ID：{secret?.enrollment.id}</Typography.Text>
          <Typography.Text>Challenge ID：{secret?.enrollment.challengeId}</Typography.Text>
          <Typography.Text copyable={{ text: secret?.token }}>
            {t('networkAccess.proxyRuntime.token', '登记令牌')}：{secret?.token}
          </Typography.Text>
        </div>
      </Modal>
    </main>
  )
}
