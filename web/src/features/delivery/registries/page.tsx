import { useRef, useState } from 'react'
import { App, Button, Form, Input, Modal, Popconfirm, Select, Space, Switch } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback, MutationFeedback } from '@/components/request-feedback'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeliveryTable } from '../delivery-table'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import type { RegistryInput, RegistryRecord } from '../types'

type ColumnProps<T> = TableColumnsType<T>[number]

export function RegistriesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm<RegistryInput>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<RegistryRecord | null>(null)
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateRegistry = hasPermission(permissionSnapshot, 'delivery.registries.create')
  const canUpdateRegistry = hasPermission(permissionSnapshot, 'delivery.registries.update')
  const canDeleteRegistry = hasPermission(permissionSnapshot, 'delivery.registries.delete')

  const registriesQuery = useQuery(deliveryQueries.registries.list())
  const createMutation = useMutation(deliveryMutations.registries.create(queryClient))
  const updateMutation = useMutation(deliveryMutations.registries.update(queryClient))
  const deleteMutation = useMutation(deliveryMutations.registries.delete(queryClient))

  const submitting = useRef(false)
  const locked = [createMutation, updateMutation, deleteMutation].some(
    (mutation) => mutation.isPending || writeOutcomeUnknown(mutation.error),
  )
  const closeModal = () => {
    setModalVisible(false)
    setEditing(null)
  }

  const handleSubmit = async (payload: RegistryInput) => {
    if (submitting.current || locked || (editing ? !canUpdateRegistry : !canCreateRegistry)) return
    submitting.current = true
    try {
      if (editing) await updateMutation.mutateAsync({ id: editing.id, payload })
      else await createMutation.mutateAsync(payload)
      message.success(editing ? '仓库更新成功' : '仓库创建成功')
      closeModal()
    } catch {
      /* Keep credential errors on the form without echoing submitted secrets. */
    } finally {
      submitting.current = false
    }
  }

  const columns: ColumnProps<RegistryRecord>[] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '类型',
      dataIndex: 'registryType',
      render: (registryType: string) => <MetadataTag label={registryType} />,
    },
    { title: 'Endpoint', dataIndex: 'endpoint', ellipsis: true },
    { title: '命名空间', dataIndex: 'namespace', render: (value?: string) => value || '-' },
    { title: '用户名', dataIndex: 'username', render: (value?: string) => value || '-' },
    {
      title: '凭据',
      dataIndex: 'metadata',
      render: (_: RegistryRecord['metadata'], record: RegistryRecord) => (
        <BooleanTag
          value={record.metadata.secretConfigured}
          trueLabel="已配置"
          falseLabel="未配置"
        />
      ),
    },
    {
      ...tableColumnPresets.status,
      title: 'TLS 校验',
      dataIndex: 'insecure',
      render: (insecure: boolean) => (
        <BooleanTag value={!insecure} trueLabel="校验" falseLabel="跳过" falseColor="warning" />
      ),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: RegistryRecord) => (
        <Space className="soha-row-action-icons" size={2}>
          {canUpdateRegistry ? (
            <ManagementIconButton
              aria-label="编辑仓库"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => {
                setEditing(record)
                setModalVisible(true)
              }}
            />
          ) : null}
          {canDeleteRegistry ? (
            <Popconfirm
              title="确认删除？"
              disabled={locked}
              onConfirm={async () => {
                if (submitting.current || locked || !canDeleteRegistry) return
                submitting.current = true
                try {
                  await deleteMutation.mutateAsync(record.id)
                  message.success('仓库已删除')
                } catch {
                  /* The list retains the deletion feedback. */
                } finally {
                  submitting.current = false
                }
              }}
              placement="topRight"
            >
              <ManagementIconButton
                aria-label="删除仓库"
                danger
                icon={<DeleteOutlined />}
                disabled={locked}
                loading={deleteMutation.isPending}
                size="small"
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
          {!canUpdateRegistry && !canDeleteRegistry ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <MutationFeedback
        operations={[
          { mutation: deleteMutation, label: '删除镜像仓库', target: deleteMutation.variables },
        ]}
      />
      <DeliveryTable
        actions={
          canCreateRegistry ? (
            <Button
              icon={<PlusOutlined />}
              type="primary"
              onClick={() => {
                setEditing(null)
                setModalVisible(true)
              }}
            >
              添加仓库
            </Button>
          ) : null
        }
        refreshing={registriesQuery.isFetching}
        onRefresh={() => void registriesQuery.refetch()}
        isError={registriesQuery.isError}
        error={registriesQuery.error}
        hasReadData={registriesQuery.data !== undefined}
        errorDescription="暂时无法读取镜像仓库。"
        onRetry={() => void registriesQuery.refetch()}
        localSorting
        columns={columns}
        dataSource={registriesQuery.data ?? []}
        rowKey="id"
        loading={registriesQuery.isLoading}
      />
      <Modal
        title={editing ? '编辑仓库' : '添加仓库'}
        open={modalVisible}
        onCancel={closeModal}
        footer={null}
        destroyOnHidden
      >
        <MutationFeedback
          operations={[
            {
              mutation: createMutation,
              label: '创建镜像仓库',
              target: createMutation.variables?.name,
            },
            {
              mutation: updateMutation,
              label: '更新镜像仓库',
              target: updateMutation.variables?.id,
            },
          ]}
        />
        {writeOutcomeUnknown(createMutation.error) || writeOutcomeUnknown(updateMutation.error) ? (
          <>
            <Button
              loading={registriesQuery.isFetching}
              onClick={() => void registriesQuery.refetch()}
            >
              读取仓库记录核对
            </Button>
            <ReadFeedback query={registriesQuery} label="镜像仓库记录" />
            <Space orientation="vertical" style={{ width: '100%', marginTop: 12 }}>
              {registriesQuery.isSuccess
                ? registriesQuery.data
                    ?.filter((item) =>
                      editing
                        ? item.id === editing.id
                        : item.name === createMutation.variables?.name,
                    )
                    .map((item) => (
                      <span key={item.id}>
                        {item.name} · {item.endpoint} · {item.id}
                      </span>
                    ))
                : null}
            </Space>
          </>
        ) : null}
        <Form
          form={form}
          key={editing?.id ?? 'create-registry'}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={
            editing
              ? {
                  name: editing.name,
                  registryType: editing.registryType,
                  endpoint: editing.endpoint,
                  namespace: editing.namespace,
                  username: editing.username,
                  insecure: editing.insecure,
                  metadata: {
                    allowedCIDRs: editing.metadata.allowedCIDRs,
                    caCertificate: editing.metadata.caCertificate,
                    authEndpoint: editing.metadata.authEndpoint,
                  },
                }
              : { insecure: false }
          }
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="registryType"
            label="类型"
            rules={[{ required: true, message: '请选择类型' }]}
          >
            <Select
              options={[
                { value: 'docker', label: 'Docker Hub' },
                { value: 'harbor', label: 'Harbor' },
                { value: 'acr', label: 'ACR' },
                { value: 'ecr', label: 'ECR' },
                { value: 'gcr', label: 'GCR' },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="endpoint"
            label="Endpoint"
            rules={[{ required: true, message: '请输入地址' }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="username" label="用户名">
            <Input />
          </Form.Item>
          <Form.Item name="namespace" label="命名空间">
            <Input />
          </Form.Item>
          <Form.Item name="secret" label={editing ? '凭据（留空不修改）' : '凭据'}>
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="insecure" label="跳过 TLS 证书校验" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            name={['metadata', 'allowedCIDRs']}
            label="允许访问的内网 CIDR"
            extra="镜像核验访问内网时必须明确配置，多个范围用逗号分隔。"
            rules={[{ max: 2048 }]}
          >
            <Input placeholder="10.20.0.0/16" />
          </Form.Item>
          <Form.Item
            name={['metadata', 'caCertificate']}
            label="镜像核验 CA 证书"
            rules={[{ max: 65536 }]}
          >
            <Input.TextArea rows={3} placeholder="PEM CA 证书；镜像核验始终校验证书" />
          </Form.Item>
          <Form.Item
            name={['metadata', 'authEndpoint']}
            label="独立认证服务地址"
            extra="仅在 registry 使用独立 token 服务时填写其 HTTP(S) origin。"
            rules={[{ max: 2048 }]}
          >
            <Input placeholder="https://auth.example.com" />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={closeModal}>取消</Button>
            <Button
              htmlType="submit"
              type="primary"
              disabled={locked}
              loading={createMutation.isPending || updateMutation.isPending}
            >
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
