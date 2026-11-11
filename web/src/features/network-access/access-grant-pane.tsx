import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined, StopOutlined } from '@ant-design/icons'
import type {
  NetworkAccessGrant,
  NetworkAccessGrantInput,
  NetworkAccessGrantSecret,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Typography,
} from 'antd'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'
import { TablePane } from './table-pane'
import { NetworkStatusTag } from './network-status-tag'
import { formatDateTime } from '@/utils/time'
interface NetworkAccessGrantPaneProps {
  canCreate: boolean
  canRevoke: boolean
}
type GrantFormValues = Omit<NetworkAccessGrantInput, 'resourceIds'> & {
  resourceIdsText: string
}
const LIST_FILTER = { limit: 200 } as const
export function NetworkAccessGrantPane({ canCreate, canRevoke }: NetworkAccessGrantPaneProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const grants = useQuery(networkAccessQueries.accessGrants(LIST_FILTER))
  const visibleGrantsData = grants.isEnabled === false ? undefined : readableQueryData(grants)
  const createGrant = useOperationMutation(
    {
      ...networkAccessMutations.accessGrants.create(queryClient),
    },
    {
      label: ['创建 访问授权', 'Create access grant'],
      target: (v) => [v.siteId, v.deviceId].filter(Boolean).join(' / '),
    },
  )
  const revokeGrant = useOperationMutation(
    {
      ...networkAccessMutations.accessGrants.revoke(queryClient),
    },
    { label: ['撤销 访问授权', 'Revoke access grant'], target: (v) => v },
  )
  const operationBlocked =
    createGrant.blocked ||
    revokeGrant.blocked ||
    (grants.isEnabled !== false && queryAccessDenied(grants))
  const [createOpen, setCreateOpen] = useState(false)
  const [secret, setSecret] = useState<NetworkAccessGrantSecret | null>(null)
  const [form] = Form.useForm<GrantFormValues>()

  const canRevealSecret = canCreate && grants.isEnabled !== false && !queryAccessDenied(grants)
  const secretAccess = useRef({ allowed: canRevealSecret, generation: 0 })
  useLayoutEffect(() => {
    secretAccess.current.allowed = canRevealSecret
    if (!canRevealSecret) {
      secretAccess.current.generation += 1
      setSecret(null)
      setCreateOpen(false)
      form.resetFields()
    }
  }, [canRevealSecret, form])
  useLayoutEffect(
    () => () => {
      secretAccess.current.allowed = false
      secretAccess.current.generation += 1
    },
    [],
  )
  useEffect(() => {
    if (!canRevealSecret && createGrant.isSuccess) createGrant.reset()
  }, [canRevealSecret, createGrant])
  function openCreate() {
    if (operationBlocked) return
    form.resetFields()
    form.setFieldsValue({ mode: 'internal_ztna', ttlSeconds: 300 })
    setCreateOpen(true)
  }
  function closeCreate() {
    setCreateOpen(false)
    form.resetFields()
  }
  async function submitCreate() {
    const generation = secretAccess.current.generation
    const values = await form.validateFields()
    if (!secretAccess.current.allowed || generation !== secretAccess.current.generation) return
    const resourceIds = [...new Set(values.resourceIdsText.split(/[\s,]+/).filter(Boolean))].sort()
    const result = await createGrant.mutateAsync({
      deviceId: values.deviceId.trim(),
      siteId: values.siteId.trim(),
      networkSpaceId: values.networkSpaceId.trim(),
      mode: values.mode,
      resourceIds,
      ttlSeconds: values.ttlSeconds,
    })
    if (!secretAccess.current.allowed || generation !== secretAccess.current.generation) {
      createGrant.reset()
      return
    }
    closeCreate()
    setSecret(result)
  }
  function closeSecret() {
    setSecret(null)
    createGrant.reset()
  }
  const columns: TableColumnsType<NetworkAccessGrant> = [
    { title: t('networkAccess.grant.subjectId', '主体 ID'), dataIndex: 'subjectId' },
    { title: t('networkAccess.grant.deviceId', '设备 ID'), dataIndex: 'deviceId' },
    { title: t('networkAccess.grant.siteId', '站点 ID'), dataIndex: 'siteId' },
    {
      title: t('networkAccess.grant.mode', '访问形态'),
      dataIndex: 'mode',
      width: 180,
    },
    {
      title: t('networkAccess.grant.resources', '资源'),
      dataIndex: 'resourceIds',
      render: (value: string[]) => value.join(', '),
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <NetworkStatusTag value={value} />,
    },
    {
      title: t('networkAccess.grant.expiresAt', '过期时间'),
      dataIndex: 'expiresAt',
      render: formatDateTime,
      width: 200,
    },
    ...(canRevoke
      ? [
          {
            key: 'actions',
            width: 70,
            render: (_: unknown, item: NetworkAccessGrant) =>
              item.status === 'issued' ? (
                <Popconfirm
                  title={t('networkAccess.grant.confirmRevoke', '确认吊销此访问授权？')}
                  onConfirm={() => revokeGrant.mutate(item.id)}
                  okButtonProps={{ disabled: operationBlocked }}
                  disabled={operationBlocked}
                >
                  <ManagementIconButton
                    aria-label={t('networkAccess.grant.revoke', '吊销访问授权')}
                    danger
                    icon={<StopOutlined />}
                    tooltip={t('networkAccess.grant.revoke', '吊销访问授权')}
                  />
                </Popconfirm>
              ) : null,
          },
        ]
      : []),
  ]
  return (
    <>
      {!(
        (createOpen && !(grants.isEnabled !== false && queryAccessDenied(grants))) ||
        (Boolean(secret) && !(grants.isEnabled !== false && queryAccessDenied(grants)))
      ) && (
        <>
          {' '}
          {createGrant.feedback}
          {revokeGrant.feedback}{' '}
        </>
      )}

      <TablePane
        columns={columns}
        items={visibleGrantsData}
        loading={grants.isLoading}
        error={grants.isEnabled === false ? null : grants.error}
        refreshing={grants.isFetching}
        onRefresh={() => void (grants.isEnabled === false ? undefined : grants.refetch())}
        searchPlaceholder={t('networkAccess.grant.search', '搜索主体、设备、站点、资源或状态')}
        getSearchValues={(item) => [
          item.id,
          item.subjectId,
          item.deviceId,
          item.siteId,
          item.networkSpaceId,
          item.mode,
          item.status,
          ...item.resourceIds,
        ]}
        createAction={
          canCreate ? (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={openCreate}
              disabled={operationBlocked}
            >
              {t('networkAccess.grant.create', '创建访问授权')}
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
        open={createOpen && !(grants.isEnabled !== false && queryAccessDenied(grants))}
        title={t('networkAccess.grant.create', '创建访问授权')}
        confirmLoading={createGrant.isPending}
        okText={t('common.create', '创建')}
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) closeCreate()
        }}
        onOk={() => void submitCreate().catch(() => undefined)}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createGrant.feedback}
        {revokeGrant.feedback}

        <Alert
          showIcon
          type="info"
          title={t('networkAccess.grant.mfaRequired', '创建前需要近期完成 MFA 验证')}
          style={{ marginBottom: 16 }}
        />
        <Form form={form} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="deviceId"
            label={t('networkAccess.grant.deviceId', '设备 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="siteId"
            label={t('networkAccess.grant.siteId', '站点 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="networkSpaceId"
            label={t('networkAccess.grant.networkSpaceId', '网络空间 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="mode"
            label={t('networkAccess.grant.mode', '访问形态')}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                {
                  value: 'internal_ztna',
                  label: t('networkAccess.mode.internalZtna', '内网 ZTNA'),
                },
                {
                  value: 'external_vpn_ztna',
                  label: t('networkAccess.mode.externalVpnZtna', '外部 VPN + ZTNA'),
                },
                {
                  value: 'external_direct_ztna',
                  label: t('networkAccess.mode.externalDirectZtna', '外部直接 ZTNA'),
                },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="resourceIdsText"
            label={t('networkAccess.grant.resources', '资源 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input.TextArea maxLength={33023} rows={3} />
          </Form.Item>
          <Form.Item
            name="ttlSeconds"
            label={t('networkAccess.grant.ttlSeconds', '有效期（秒）')}
            rules={[{ required: true }]}
          >
            <InputNumber min={60} max={300} step={60} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        destroyOnHidden
        cancelButtonProps={{ ...{ style: { display: 'none' } }, disabled: operationBlocked }}
        open={Boolean(secret) && !(grants.isEnabled !== false && queryAccessDenied(grants))}
        title={t('networkAccess.grant.secretTitle', '一次性访问授权')}
        okText={t('networkAccess.grant.saved', '我已保存')}
        onCancel={() => {
          if (!operationBlocked) closeSecret()
        }}
        onOk={closeSecret}
        closable={!operationBlocked}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createGrant.feedback}
        {revokeGrant.feedback}

        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Alert
            showIcon
            type="warning"
            title={t('networkAccess.grant.secretOnce', '明文授权令牌只展示一次')}
            description={t(
              'networkAccess.grant.secretHint',
              '关闭后无法再次查看；令牌将在首次连接后失效。',
            )}
          />
          <Typography.Text type="secondary">
            {secret?.grant.id} · {secret?.grant.expiresAt}
          </Typography.Text>
          <Typography.Text
            code
            copyable={{ text: secret?.token ?? '' }}
            style={{ wordBreak: 'break-all' }}
          >
            {secret?.token}
          </Typography.Text>
        </Space>
      </Modal>
    </>
  )
}
