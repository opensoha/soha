import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined, QuestionCircleOutlined, StopOutlined } from '@ant-design/icons'
import type {
  NetworkRuntimeEnrollment,
  NetworkRuntimeEnrollmentInput,
  NetworkRuntimeEnrollmentSecret,
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
  Popover,
  Select,
  Space,
  Typography,
} from 'antd'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'
import { MetadataTag } from '@/components/status-tag'
import { TablePane } from './table-pane'
import { NetworkStatusTag } from './network-status-tag'
import { formatDateTime } from '@/utils/time'
interface NetworkEnrollmentPaneProps {
  canCreate: boolean
  canRevoke: boolean
  runtimeKind?: NetworkRuntimeEnrollmentInput['runtimeKind']
}
const LIST_FILTER = { limit: 200 } as const
export function NetworkEnrollmentPane({
  canCreate,
  canRevoke,
  runtimeKind,
}: NetworkEnrollmentPaneProps) {
  const { localeCode, t } = useI18n()
  const queryClient = useQueryClient()
  const enrollments = useQuery(networkAccessQueries.enrollments(LIST_FILTER))
  const visibleEnrollmentsData =
    enrollments.isEnabled === false ? undefined : readableQueryData(enrollments)
  const createEnrollment = useOperationMutation(
    networkAccessMutations.enrollments.create(queryClient),
    { label: ['创建 运行时登记', 'Create runtime enrollment'], target: (v) => v.deviceId },
  )
  const revokeEnrollment = useOperationMutation(
    networkAccessMutations.enrollments.revoke(queryClient),
    { label: ['撤销 运行时登记', 'Revoke runtime enrollment'], target: (v) => v },
  )
  const operationBlocked =
    createEnrollment.blocked ||
    revokeEnrollment.blocked ||
    (enrollments.isEnabled !== false && queryAccessDenied(enrollments))
  const [createOpen, setCreateOpen] = useState(false)
  const [secret, setSecret] = useState<NetworkRuntimeEnrollmentSecret | null>(null)
  const [form] = Form.useForm<NetworkRuntimeEnrollmentInput>()

  const canRevealSecret =
    canCreate && enrollments.isEnabled !== false && !queryAccessDenied(enrollments)
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
    if (!canRevealSecret && createEnrollment.isSuccess) createEnrollment.reset()
  }, [canRevealSecret, createEnrollment])
  const isRadius = runtimeKind === 'nas'
  function openCreate() {
    if (operationBlocked) return
    form.resetFields()
    form.setFieldsValue({ runtimeKind: runtimeKind ?? 'endpoint', ttlSeconds: 600 })
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
    const result = await createEnrollment.mutateAsync({
      runtimeId: values.runtimeId.trim(),
      runtimeKind: runtimeKind ?? values.runtimeKind,
      deviceId: values.deviceId.trim(),
      subjectId: values.subjectId.trim(),
      ttlSeconds: values.ttlSeconds,
    })
    if (!secretAccess.current.allowed || generation !== secretAccess.current.generation) {
      createEnrollment.reset()
      return
    }
    closeCreate()
    setSecret(result)
  }
  function closeSecret() {
    setSecret(null)
    createEnrollment.reset()
  }
  const columns: TableColumnsType<NetworkRuntimeEnrollment> = [
    { title: t('networkAccess.enrollment.runtimeId', '运行时 ID'), dataIndex: 'runtimeId' },
    ...(isRadius
      ? []
      : [
          {
            title: t('networkAccess.enrollment.runtimeKind', '运行时类型'),
            dataIndex: 'runtimeKind',
            width: 120,
            render: (value: string) => {
              const labels: Record<string, [string, string]> = {
                endpoint: ['终端', 'Endpoint'],
                gateway: ['网关', 'Gateway'],
                nas: ['RADIUS 运行时', 'RADIUS runtime'],
                proxy: ['代理', 'Proxy'],
              }
              return (
                <MetadataTag
                  label={t(
                    `networkAccess.runtimeKind.${value}`,
                    labels[value]?.[localeCode === 'zh_CN' ? 0 : 1] ?? value,
                  )}
                />
              )
            },
          },
        ]),
    { title: t('networkAccess.enrollment.deviceId', '设备 ID'), dataIndex: 'deviceId' },
    { title: t('networkAccess.enrollment.subjectId', '主体 ID'), dataIndex: 'subjectId' },
    {
      title: isRadius
        ? t('networkAccess.radius.registrationStatus', '注册状态')
        : t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <NetworkStatusTag value={value} />,
    },
    {
      title: t('networkAccess.enrollment.expiresAt', '过期时间'),
      dataIndex: 'expiresAt',
      render: formatDateTime,
      width: 200,
    },
    ...(canRevoke
      ? [
          {
            key: 'actions',
            width: 70,
            render: (_: unknown, item: NetworkRuntimeEnrollment) =>
              item.status === 'pending' ? (
                <Popconfirm
                  title={t('networkAccess.enrollment.confirmRevoke', '确认吊销此注册令牌？')}
                  onConfirm={() => revokeEnrollment.mutate(item.id)}
                  okButtonProps={{ disabled: operationBlocked }}
                  disabled={operationBlocked}
                >
                  <ManagementIconButton
                    aria-label={t('networkAccess.enrollment.revoke', '吊销注册令牌')}
                    danger
                    icon={<StopOutlined />}
                    tooltip={t('networkAccess.enrollment.revoke', '吊销注册令牌')}
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
        (createOpen && !(enrollments.isEnabled !== false && queryAccessDenied(enrollments))) ||
        (Boolean(secret) && !(enrollments.isEnabled !== false && queryAccessDenied(enrollments)))
      ) && (
        <>
          {' '}
          {createEnrollment.feedback}
          {revokeEnrollment.feedback}{' '}
        </>
      )}

      <TablePane
        columns={columns}
        items={visibleEnrollmentsData?.filter(
          (item) => !runtimeKind || item.runtimeKind === runtimeKind,
        )}
        loading={enrollments.isLoading}
        error={enrollments.isEnabled === false ? null : enrollments.error}
        refreshing={enrollments.isFetching}
        onRefresh={() => void (enrollments.isEnabled === false ? undefined : enrollments.refetch())}
        searchPlaceholder={t('networkAccess.enrollment.search', '搜索运行时、设备、主体或状态')}
        getSearchValues={(item) => [
          item.runtimeId,
          item.runtimeKind,
          item.deviceId,
          item.subjectId,
          item.status,
        ]}
        createAction={
          isRadius || canCreate ? (
            <Space wrap>
              {isRadius ? (
                <Popover
                  trigger="click"
                  title={t('networkAccess.radius.runtimeTitle', 'RADIUS 接入说明')}
                  content={
                    <Typography.Paragraph style={{ maxWidth: 320, marginBottom: 0 }}>
                      {t(
                        'networkAccess.radius.registrationHint',
                        '创建一次性接入令牌，再由 Docker Compose、Kubernetes 或独立 FreeRADIUS 运行时完成注册。这里展示注册令牌状态，不代表服务健康；密钥不会保存在网络设备记录中。',
                      )}
                    </Typography.Paragraph>
                  }
                >
                  <Button type="text" icon={<QuestionCircleOutlined aria-hidden />}>
                    {t('networkAccess.radius.help', '接入说明')}
                  </Button>
                </Popover>
              ) : null}
              {canCreate ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={openCreate}
                  disabled={operationBlocked}
                >
                  {isRadius
                    ? t('networkAccess.radius.createToken', '创建接入令牌')
                    : t('networkAccess.enrollment.create', '创建注册令牌')}
                </Button>
              ) : null}
            </Space>
          ) : null
        }
      />

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        open={createOpen && !(enrollments.isEnabled !== false && queryAccessDenied(enrollments))}
        title={
          isRadius
            ? t('networkAccess.radius.createToken', '创建接入令牌')
            : t('networkAccess.enrollment.create', '创建注册令牌')
        }
        confirmLoading={createEnrollment.isPending}
        okText={t('common.create', '创建')}
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) closeCreate()
        }}
        onOk={() => void submitCreate()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createEnrollment.feedback}
        {revokeEnrollment.feedback}

        <Form form={form} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="runtimeId"
            label={t('networkAccess.enrollment.runtimeId', '运行时 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          {runtimeKind ? (
            <Form.Item label={t('networkAccess.enrollment.runtimeKind', '运行时类型')}>
              <Input value={runtimeKind} disabled />
            </Form.Item>
          ) : (
            <Form.Item
              name="runtimeKind"
              label={t('networkAccess.enrollment.runtimeKind', '运行时类型')}
              rules={[{ required: true }]}
            >
              <Select
                options={['endpoint', 'gateway', 'nas'].map((value) => ({ value, label: value }))}
              />
            </Form.Item>
          )}
          <Form.Item
            name="deviceId"
            label={t('networkAccess.enrollment.deviceId', '设备 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="subjectId"
            label={t('networkAccess.enrollment.subjectId', '主体 ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="ttlSeconds"
            label={t('networkAccess.enrollment.ttlSeconds', '有效期（秒）')}
            rules={[{ required: true }]}
          >
            <InputNumber min={60} max={600} step={60} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        destroyOnHidden
        cancelButtonProps={{ ...{ style: { display: 'none' } }, disabled: operationBlocked }}
        open={
          Boolean(secret) && !(enrollments.isEnabled !== false && queryAccessDenied(enrollments))
        }
        title={t('networkAccess.enrollment.secretTitle', '一次性注册令牌')}
        okText={t('networkAccess.enrollment.saved', '我已保存')}
        onCancel={() => {
          if (!operationBlocked) closeSecret()
        }}
        onOk={closeSecret}
        closable={!operationBlocked}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createEnrollment.feedback}
        {revokeEnrollment.feedback}

        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Alert
            showIcon
            type="warning"
            title={t('networkAccess.enrollment.secretOnce', '明文令牌只展示一次')}
            description={t(
              'networkAccess.enrollment.secretHint',
              '关闭后无法再次查看；如未完成注册，请吊销并重新创建。',
            )}
          />
          <Typography.Text type="secondary">
            {secret?.enrollment.runtimeId} · {secret?.enrollment.expiresAt}
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
