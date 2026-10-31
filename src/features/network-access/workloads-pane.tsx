import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { BooleanTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { NetworkWorkloadBinding } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, Modal, Switch, Typography, type TableColumnsType } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'
import { TablePane } from './table-pane'

type Values = Omit<NetworkWorkloadBinding, 'resourceIds' | 'revision'> & { resourcesText: string }

export function NetworkWorkloadsPane({ canUpdate }: { canUpdate: boolean }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const query = useQuery(networkAccessQueries.workloadBindings())
  const items = readableQueryData(query)
  const mutation = useOperationMutation(networkAccessMutations.workloadBindings.put(queryClient), {
    label: ['保存工作负载绑定', 'Save workload binding'],
    target: (value) => value.id,
  })
  const [editing, setEditing] = useState<NetworkWorkloadBinding | null | undefined>(undefined)
  const [form] = Form.useForm<Values>()
  const generation = useRef(0)
  const denied = queryAccessDenied(query)
  const blocked = mutation.blocked || denied || !canUpdate
  const open = editing !== undefined && !denied && canUpdate
  useEffect(() => {
    if (!canUpdate || denied) {
      generation.current += 1
      setEditing(undefined)
      form.resetFields()
    }
  }, [canUpdate, denied, form])
  function close() {
    generation.current += 1
    setEditing(undefined)
    form.resetFields()
  }
  function edit(item: NetworkWorkloadBinding | null) {
    if (blocked) return
    generation.current += 1
    form.resetFields()
    setEditing(item)
    form.setFieldsValue(
      item
        ? { ...item, resourcesText: item.resourceIds.join('\n') }
        : {
            id: crypto.randomUUID(),
            enabled: true,
            expiresAt: new Date(Date.now() + 86400000).toISOString(),
          },
    )
  }
  async function save() {
    const current = generation.current
    const value = await form.validateFields()
    if (blocked || current !== generation.current) return
    const input = {
      serviceAccountId: value.serviceAccountId.trim(),
      runtimeId: value.runtimeId.trim(),
      deviceId: value.deviceId.trim(),
      siteId: value.siteId.trim(),
      networkSpaceId: value.networkSpaceId.trim(),
      resourceIds: value.resourcesText.split(/[\s,]+/).filter(Boolean),
      enabled: value.enabled,
      expiresAt: new Date(value.expiresAt).toISOString(),
      revision: editing?.revision ?? 0,
    }
    await mutation.mutateAsync({ id: value.id.trim(), input })
    if (current === generation.current) close()
  }
  const fields: [keyof Values, string][] = [
    ['id', t('networkAccess.workloads.id', '绑定 ID')],
    ['serviceAccountId', t('networkAccess.workloads.serviceAccountId', '服务账户 ID')],
    ['runtimeId', t('networkAccess.enrollment.runtimeId', '运行时 ID')],
    ['deviceId', t('networkAccess.enrollment.deviceId', '设备 ID')],
    ['siteId', t('networkAccess.siteId', '站点 ID')],
    ['networkSpaceId', t('networkAccess.workloads.spaceId', '网络空间 ID')],
  ]
  const columns: TableColumnsType<NetworkWorkloadBinding> = [
    { title: t('networkAccess.workloads.id', '绑定 ID'), dataIndex: 'id' },
    {
      title: t('networkAccess.workloads.serviceAccountId', '服务账户 ID'),
      dataIndex: 'serviceAccountId',
    },
    { title: t('networkAccess.enrollment.deviceId', '设备 ID'), dataIndex: 'deviceId' },
    {
      title: t('networkAccess.workloads.resources', '资源范围'),
      dataIndex: 'resourceIds',
      render: (ids: string[]) => ids.join(', '),
    },
    {
      title: t('networkAccess.workloads.expiresAt', '到期时间'),
      dataIndex: 'expiresAt',
      render: formatDateTime,
    },
    {
      title: t('common.enabled', '启用'),
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} />,
    },
    ...(canUpdate
      ? [
          {
            key: 'actions',
            width: 70,
            render: (_: unknown, item: NetworkWorkloadBinding) => (
              <ManagementIconButton
                aria-label={t('common.edit', '编辑')}
                tooltip={t('common.edit', '编辑')}
                icon={<EditOutlined />}
                disabled={blocked}
                onClick={() => edit(item)}
              />
            ),
          },
        ]
      : []),
  ]
  return (
    <>
      <Typography.Paragraph type="secondary">
        {t(
          'networkAccess.workloads.hint',
          '绑定仅限定授权上限。每个资源还需发布明确匹配 service_account:ID 与资源 ID 的允许策略；禁用或修改绑定后必须重新登记。工作负载使用短期机器凭据，不代用人的 MFA。',
        )}
      </Typography.Paragraph>
      {!open && mutation.feedback}
      <TablePane
        columns={columns}
        items={items}
        loading={query.isLoading}
        error={query.error}
        refreshing={query.isFetching}
        onRefresh={() => void query.refetch()}
        getSearchValues={(item) => [
          item.id,
          item.serviceAccountId,
          item.deviceId,
          ...item.resourceIds,
        ]}
        searchPlaceholder={t('networkAccess.workloads.search', '搜索绑定、服务账户、设备或资源')}
        createAction={
          canUpdate ? (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={blocked}
              onClick={() => edit(null)}
            >
              {t('networkAccess.workloads.create', '新增工作负载绑定')}
            </Button>
          ) : null
        }
      />
      <Modal
        styles={operationModalStyles}
        destroyOnHidden
        open={open}
        title={t('networkAccess.workloads.title', '工作负载绑定')}
        confirmLoading={mutation.isPending}
        onOk={() => void save()}
        onCancel={() => {
          if (!mutation.isPending) close()
        }}
        closable={!mutation.isPending}
        cancelButtonProps={{ disabled: mutation.isPending }}
        okButtonProps={{ disabled: blocked }}
        mask={{ closable: false }}
      >
        {mutation.feedback}
        <Form form={form} layout="vertical" disabled={blocked}>
          {fields.map(([name, label]) => (
            <Form.Item
              key={name}
              name={name}
              label={label}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input maxLength={128} disabled={Boolean(editing)} />
            </Form.Item>
          ))}
          <Form.Item
            name="resourcesText"
            label={t('networkAccess.workloads.resources', '资源范围')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input.TextArea autoSize={{ minRows: 3, maxRows: 8 }} maxLength={33024} />
          </Form.Item>
          <Form.Item
            name="expiresAt"
            label={t('networkAccess.workloads.expiresAt', '到期时间')}
            rules={[
              { required: true },
              {
                validator: (_, value) =>
                  Number.isFinite(Date.parse(String(value)))
                    ? Promise.resolve()
                    : Promise.reject(
                        new Error(
                          t('networkAccess.workloads.invalidDate', '请输入有效的 ISO 日期时间'),
                        ),
                      ),
              },
            ]}
          >
            <Input maxLength={40} />
          </Form.Item>
          <Form.Item name="enabled" label={t('common.enabled', '启用')} valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
