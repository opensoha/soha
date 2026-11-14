import { scrollableModalBodyStyle, viewportModalStyle } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  createS3StorageIntegration,
  inferS3StoragePreset,
  s3StorageFormValues,
  systemIntegrationMutations,
  systemIntegrationQueries,
  updateS3StorageIntegration,
  type S3StorageFormValues,
  type S3StoragePreset,
  type SystemIntegration,
  type SystemIntegrationCategory,
} from '@/features/settings'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  DeleteOutlined,
  EditOutlined,
  HddOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Badge,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useState } from 'react'
import { softwarePackageQueries } from '../queries'
import type { SoftwareStorage } from '../types'
const { Text } = Typography
type StorageHealthStatus = 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
const presetOptions: Array<{
  label: string
  value: S3StoragePreset
}> = [
  { label: 'Amazon S3', value: 'aws' },
  { label: 'MinIO', value: 'minio' },
  { label: '阿里云 OSS（S3 兼容）', value: 'oss' },
  { label: '腾讯云 COS（S3 兼容）', value: 'cos' },
  { label: '其他 S3 兼容存储', value: 'custom' },
]
const endpointPlaceholders: Record<S3StoragePreset, string> = {
  aws: 'AWS S3 可留空',
  minio: 'https://minio.example.com',
  oss: 'https://oss-cn-hangzhou.aliyuncs.com',
  cos: 'https://cos.ap-shanghai.myqcloud.com',
  custom: 'https://s3.example.com',
}
export function ObjectStorageConfig({ storage }: { storage?: SoftwareStorage }) {
  const queryClient = useQueryClient()
  const [form] = Form.useForm<S3StorageFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<SystemIntegration | null>(null)
  const [testingAfterSave, setTestingAfterSave] = useState(false)
  const preset = Form.useWatch('preset', form) ?? 'minio'
  const permissions = usePermissionSnapshot().data?.data
  const canView = hasPermission(permissions, 'settings.system-integrations.view')
  const canCreate = hasPermission(permissions, 'settings.system-integrations.create')
  const canUpdate = hasPermission(permissions, 'settings.system-integrations.update')
  const canDelete = hasPermission(permissions, 'settings.system-integrations.delete')
  const canTest = hasPermission(permissions, 'settings.system-integrations.test')
  const integrationsQuery = useQuery(
    systemIntegrationQueries.list(
      { category: 'storage' as SystemIntegrationCategory, providerType: 's3' },
      canView,
    ),
  )
  const visibleIntegrationsQueryData =
    integrationsQuery.isEnabled === false ? undefined : readableQueryData(integrationsQuery)
  const integrations = visibleIntegrationsQueryData ?? []
  const enabledIntegrations = integrations.filter((item) => item.enabled)
  const healthyIntegrations = enabledIntegrations.filter((item) => item.healthStatus === 'healthy')
  const integrationStorageQueries = useQueries({
    queries: integrations.map((item) => ({
      ...softwarePackageQueries.storage(item.id),
      enabled: canView && drawerOpen,
    })),
  })
  const integrationStorage = new Map(
    integrations.map((item, index) => [
      item.id,
      integrationStorageQueries[index]?.isEnabled === false
        ? undefined
        : readableQueryData(integrationStorageQueries[index]),
    ]),
  )
  const createMutation = useOperationMutation(systemIntegrationMutations.create(queryClient), {
    label: ['创建 对象存储', 'Create object storage'],
    target: (v) => v.name,
  })
  const updateMutation = useOperationMutation(systemIntegrationMutations.update(queryClient), {
    label: ['保存 对象存储', 'Save object storage'],
    target: (v) => v.id,
  })
  const removeMutation = useOperationMutation(systemIntegrationMutations.remove(queryClient), {
    label: ['删除 对象存储', 'Delete object storage'],
    target: (v) => v,
  })
  const testMutation = useOperationMutation(systemIntegrationMutations.test(queryClient), {
    label: ['测试对象存储', 'Test object storage'],
    target: (v) => v,
    readOnly: true,
    resultError: (result) =>
      result.status === 'succeeded'
        ? undefined
        : new Error('对象存储连接测试失败；配置未因此变更。'),
    receipt: () => ({ message: '对象存储连接测试成功', evidence: '此结果仅反映本次连接测试。' }),
  })
  const operationBlocked =
    createMutation.blocked ||
    updateMutation.blocked ||
    removeMutation.blocked ||
    testMutation.blocked ||
    (integrationsQuery.isEnabled !== false && queryAccessDenied(integrationsQuery))
  const saving = createMutation.isPending || updateMutation.isPending
  useEffect(() => {
    if (modalOpen) {
      form.resetFields()
      form.setFieldsValue(s3StorageFormValues(editing ?? undefined))
    }
  }, [editing, form, modalOpen])
  const closeModal = () => {
    setModalOpen(false)
    setEditing(null)
  }
  const finishSave = (saved: SystemIntegration, testAfterSave: boolean) => {
    if (!testAfterSave) {
      closeModal()
      return
    }
    testMutation.mutate(saved.id, {
      onSuccess: () => {
        setTestingAfterSave(false)
        closeModal()
      },
      onError: () => {
        setTestingAfterSave(false)
        closeModal()
      },
    })
  }
  const save = (values: S3StorageFormValues, testAfterSave = false) => {
    if (editing) {
      if (!canUpdate) return
      updateMutation.mutate(
        { id: editing.id, values: updateS3StorageIntegration(editing, values) },
        {
          onSuccess: (saved) => finishSave(saved, testAfterSave),
          onError: () => {
            setTestingAfterSave(false)
          },
        },
      )
      return
    }
    if (!canCreate) return
    createMutation.mutate(createS3StorageIntegration(values), {
      onSuccess: (saved) => finishSave(saved, testAfterSave),
      onError: () => {
        setTestingAfterSave(false)
      },
    })
  }
  const testConnection = (integration: SystemIntegration) => {
    testMutation.mutate(integration.id)
  }
  if (!canView) return null
  return (
    <>
      {!(
        (drawerOpen &&
          !(integrationsQuery.isEnabled !== false && queryAccessDenied(integrationsQuery))) ||
        (modalOpen &&
          !(integrationsQuery.isEnabled !== false && queryAccessDenied(integrationsQuery)))
      ) && (
        <>
          {' '}
          {createMutation.feedback}
          {updateMutation.feedback}
          {removeMutation.feedback}
          {testMutation.feedback}{' '}
        </>
      )}

      <ReadFeedback
        canRead={
          integrationsQuery.isEnabled && (!drawerOpen || queryAccessDenied(integrationsQuery))
        }
        query={integrationsQuery}
        label="身份数据"
      />

      <Button
        autoInsertSpace={false}
        icon={<HddOutlined />}
        loading={integrationsQuery.isLoading}
        size="small"
        onClick={() => setDrawerOpen(true)}
        disabled={operationBlocked}
      >
        对象存储
      </Button>
      <Drawer
        destroyOnHidden
        loading={integrationsQuery.isLoading}
        open={
          drawerOpen &&
          !(integrationsQuery.isEnabled !== false && queryAccessDenied(integrationsQuery))
        }
        size={900}
        title={
          <Space size={8}>
            <span>对象存储</span>
            <StorageHealthIndicator
              enabled={enabledIntegrations.length > 0}
              status={aggregateHealthStatus(enabledIntegrations)}
            />
          </Space>
        }
        extra={
          <Button
            disabled={operationBlocked || !canCreate || integrationsQuery.isError}
            icon={<PlusOutlined />}
            onClick={() => {
              setEditing(null)
              setModalOpen(true)
            }}
          >
            新增存储
          </Button>
        }
        onClose={() => {
          if (!operationBlocked) (() => setDrawerOpen(false))()
        }}
      >
        <ReadFeedback
          canRead={integrationsQuery.isEnabled && !modalOpen}
          query={integrationsQuery}
          label="对象存储"
        />
        {createMutation.feedback}
        {updateMutation.feedback}
        {removeMutation.feedback}
        {testMutation.feedback}

        {integrationsQuery.isError && !visibleIntegrationsQueryData ? null : (
          <div className="soha-software-object-storage-config">
            <Descriptions
              column={{ xs: 2, sm: 4 }}
              items={[
                { key: 'instances', label: '存储实例', children: integrations.length },
                {
                  key: 'enabled',
                  label: '已启用',
                  children: `${enabledIntegrations.length}（${healthyIntegrations.length} 正常）`,
                },
                { key: 'files', label: '文件总数', children: storage?.objectCount ?? '-' },
                {
                  key: 'usage',
                  label: '总已用空间',
                  children: storage ? formatBytes(storage.totalBytes) : '-',
                },
              ]}
              size="small"
            />
            <Table<SystemIntegration>
              columns={[
                {
                  title: '存储实例',
                  key: 'name',
                  render: (_, item) => (
                    <Space size={8}>
                      <StorageHealthIndicator
                        enabled={item.enabled}
                        status={storageHealthStatus(item)}
                      />
                      <Space orientation="vertical" size={0}>
                        <Text strong>{item.name}</Text>
                        <Text type="secondary">{presetLabel(inferS3StoragePreset(item))}</Text>
                      </Space>
                    </Space>
                  ),
                },
                {
                  title: 'Endpoint / Bucket',
                  key: 'endpoint',
                  render: (_, item) => (
                    <Space orientation="vertical" size={0}>
                      <Text>{configurationValue(item, 'endpoint') || 'AWS 默认 Endpoint'}</Text>
                      <Text type="secondary">{configurationValue(item, 'bucket') || '-'}</Text>
                    </Space>
                  ),
                },
                {
                  title: '地域',
                  key: 'region',
                  width: 88,
                  render: (_, item) => configurationValue(item, 'region') || '-',
                },
                {
                  title: '文件数',
                  key: 'objectCount',
                  align: 'right',
                  width: 72,
                  render: (_, item) => integrationStorage.get(item.id)?.objectCount ?? '-',
                },
                {
                  title: '已用空间',
                  key: 'totalBytes',
                  align: 'right',
                  width: 88,
                  render: (_, item) => {
                    const totalBytes = integrationStorage.get(item.id)?.totalBytes
                    return totalBytes === undefined ? '-' : formatBytes(totalBytes)
                  },
                },
                {
                  title: '状态',
                  key: 'enabled',
                  width: 76,
                  render: (_, item) => (
                    <StatusTag
                      label={item.enabled ? '已启用' : '已停用'}
                      value={item.enabled ? 'enabled' : 'disabled'}
                    />
                  ),
                },
                {
                  title: '操作',
                  key: 'actions',
                  width: 108,
                  render: (_, item) => (
                    <Space size={0}>
                      <Button
                        aria-label={`测试 ${item.name} 连接`}
                        disabled={operationBlocked || !canTest || !item.enabled}
                        icon={<ThunderboltOutlined />}
                        loading={testMutation.isPending && testMutation.variables === item.id}
                        size="small"
                        type="text"
                        onClick={() => testConnection(item)}
                      />
                      <Button
                        aria-label={`编辑 ${item.name}`}
                        disabled={operationBlocked || !canUpdate}
                        icon={<EditOutlined />}
                        size="small"
                        type="text"
                        onClick={() => {
                          setEditing(item)
                          setModalOpen(true)
                        }}
                      />
                      <Popconfirm
                        cancelText="取消"
                        disabled={operationBlocked || !canDelete}
                        okButtonProps={{
                          ...{ danger: true, loading: removeMutation.isPending },
                          disabled: operationBlocked,
                        }}
                        okText="删除"
                        title={`删除对象存储 ${item.name}？`}
                        onConfirm={() => removeMutation.mutate(item.id)}
                      >
                        <Button
                          aria-label={`删除 ${item.name}`}
                          danger
                          disabled={!canDelete}
                          icon={<DeleteOutlined />}
                          size="small"
                          type="text"
                        />
                      </Popconfirm>
                    </Space>
                  ),
                },
              ]}
              dataSource={integrations}
              locale={{ emptyText: '尚未配置对象存储' }}
              pagination={false}
              rowKey="id"
              scroll={{ x: 760 }}
              size="small"
            />
          </div>
        )}
      </Drawer>

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        forceRender
        mask={{ closable: false }}
        open={
          modalOpen &&
          !(integrationsQuery.isEnabled !== false && queryAccessDenied(integrationsQuery))
        }
        style={viewportModalStyle}
        styles={{ body: scrollableModalBodyStyle }}
        width={760}
        zIndex={1100}
        title={
          <Space size={4}>
            <span>{editing ? '编辑对象存储' : '新增对象存储'}</span>
            <Tooltip
              title={
                editing
                  ? '已保存连接的存储位置保持不变；可更新状态、说明和访问凭据。'
                  : 'AWS S3、MinIO、阿里云 OSS 与腾讯云 COS 均通过 S3 兼容协议接入。'
              }
              trigger={['hover', 'focus']}
            >
              <QuestionCircleOutlined aria-label="对象存储配置说明" tabIndex={0} />
            </Tooltip>
          </Space>
        }
        footer={
          <Space className="soha-software-modal-footer">
            <Button disabled={saving || testMutation.isPending} onClick={closeModal}>
              取消
            </Button>
            <Button
              disabled={operationBlocked || !canTest || saving}
              icon={<ThunderboltOutlined />}
              loading={testingAfterSave || testMutation.isPending}
              onClick={() => {
                setTestingAfterSave(true)
                void form
                  .validateFields()
                  .then((values) => save(values, true))
                  .catch(() => setTestingAfterSave(false))
              }}
            >
              保存并测试
            </Button>
            <Button
              disabled={testingAfterSave || testMutation.isPending}
              loading={saving && !testingAfterSave}
              type="primary"
              onClick={() => form.submit()}
            >
              保存
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) closeModal()
        }}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        <ReadFeedback
          canRead={integrationsQuery.isEnabled}
          query={integrationsQuery}
          label="对象存储"
        />
        {createMutation.feedback}
        {updateMutation.feedback}
        {removeMutation.feedback}
        {testMutation.feedback}

        <Form
          form={form}
          initialValues={s3StorageFormValues(editing ?? undefined)}
          layout="vertical"
          onFinish={(values) => save(values)}
          disabled={operationBlocked}
        >
          <Text className="soha-software-config-section-title" strong>
            连接信息
          </Text>
          <div className="soha-software-form-grid">
            <Form.Item name="preset" label="存储服务" rules={[{ required: true }]}>
              <Select
                disabled={operationBlocked || Boolean(editing)}
                options={presetOptions}
                onChange={(value: S3StoragePreset) =>
                  form.setFieldValue('pathStyle', !['aws', 'oss', 'cos'].includes(value))
                }
              />
            </Form.Item>
            <Form.Item name="name" label="连接名称" rules={[{ required: true }]}>
              <Input maxLength={200} />
            </Form.Item>
            <Form.Item name="region" label="Region" rules={[{ required: true }]}>
              <Input disabled={operationBlocked || Boolean(editing)} maxLength={128} />
            </Form.Item>
            <Form.Item
              name="endpoint"
              label={
                <Space size={4}>
                  Endpoint
                  <Tooltip title="云厂商填写对应地域的 S3 兼容 Endpoint；Amazon S3 可留空。">
                    <QuestionCircleOutlined aria-label="Endpoint 配置说明" tabIndex={0} />
                  </Tooltip>
                </Space>
              }
              rules={[{ type: 'url', warningOnly: true }]}
            >
              <Input
                disabled={operationBlocked || Boolean(editing)}
                placeholder={endpointPlaceholders[preset]}
              />
            </Form.Item>
            <Form.Item name="bucket" label="Bucket" rules={[{ required: true }]}>
              <Input disabled={operationBlocked || Boolean(editing)} maxLength={255} />
            </Form.Item>
            <Form.Item name="prefix" label="对象前缀">
              <Input
                disabled={operationBlocked || Boolean(editing)}
                maxLength={512}
                placeholder="可选"
              />
            </Form.Item>
          </div>
          <Text className="soha-software-config-section-title" strong>
            连接选项
          </Text>
          <div className="soha-software-switch-grid">
            <Form.Item name="pathStyle" label="Path-style" valuePropName="checked">
              <Switch disabled={operationBlocked || Boolean(editing)} />
            </Form.Item>
            <Form.Item name="insecure" label="允许 HTTP（仅内网开发）" valuePropName="checked">
              <Switch disabled={operationBlocked || Boolean(editing)} />
            </Form.Item>
            <Form.Item name="allowPrivate" label="允许私网 Endpoint" valuePropName="checked">
              <Switch disabled={operationBlocked || Boolean(editing)} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
          <Text className="soha-software-config-section-title" strong>
            访问凭据
          </Text>
          <Form.Item
            name="accessKeyId"
            label={editing ? 'Access Key ID（留空保持不变）' : 'Access Key ID'}
            rules={editing ? undefined : [{ required: true }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="secretAccessKey"
            label={editing ? 'Secret Access Key（留空保持不变）' : 'Secret Access Key'}
            rules={editing ? undefined : [{ required: true }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="sessionToken" label="Session Token（可选）">
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea maxLength={1000} rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
function StorageHealthIndicator({
  enabled,
  status,
}: {
  enabled: boolean
  status?: StorageHealthStatus
}) {
  const label = !enabled
    ? '未启用'
    : status === 'healthy'
      ? '连接正常'
      : status === 'unhealthy'
        ? '连接失败'
        : status === 'degraded'
          ? '连接降级'
          : '等待检测'
  const badgeStatus = !enabled
    ? 'default'
    : status === 'healthy'
      ? 'success'
      : status === 'unhealthy'
        ? 'error'
        : 'warning'
  return (
    <Tooltip title={label}>
      <span aria-label={label} role="status" tabIndex={0}>
        <Badge status={badgeStatus} />
      </span>
    </Tooltip>
  )
}
function aggregateHealthStatus(items: SystemIntegration[]): StorageHealthStatus {
  if (items.some((item) => storageHealthStatus(item) === 'unhealthy')) return 'unhealthy'
  if (items.some((item) => storageHealthStatus(item) === 'degraded')) return 'degraded'
  if (items.length > 0 && items.every((item) => storageHealthStatus(item) === 'healthy')) {
    return 'healthy'
  }
  return 'unknown'
}
function storageHealthStatus(item: SystemIntegration): StorageHealthStatus {
  return item.healthStatus as StorageHealthStatus
}
function presetLabel(preset: S3StoragePreset) {
  return presetOptions.find((option) => option.value === preset)?.label ?? 'S3 兼容存储'
}
function formatBytes(value: number) {
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}
function configurationValue(item: SystemIntegration | undefined, key: string) {
  return item?.configuration.find((field) => field.key === key)?.value ?? ''
}
