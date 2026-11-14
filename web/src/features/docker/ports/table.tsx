import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback, ComputeOperationFeedback } from '@/features/compute'
import { useState } from 'react'
import {
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { scrollableModalBodyStyle } from '@/components/modal-styles'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import type { DockerPortMapping, DockerPortMappingInput } from '../docker-types'
import {
  DockerAdminTable,
  compactRecord,
  formatAccessURL,
  formatPort,
  normalizePage,
  pageTablePagination,
  refreshDocker,
  statusTag,
  type DockerFilterState,
  useDockerOptions,
  useDockerPermissions,
} from '../shared/ui'
import { formatDateTime } from '@/utils/time'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'

const { Text } = Typography

interface DockerPortFormValues extends Omit<DockerPortMappingInput, 'expiresAt'> {
  expiresAt?: string
}

function buildPortPayload(values: DockerPortFormValues): DockerPortMappingInput {
  return compactRecord({
    ...values,
    protocol: values.protocol || 'tcp',
    exposureScope: values.exposureScope || 'internal',
    status: values.status || 'active',
  })
}

export function PortsTable({
  embedded = false,
  fixedHostId,
  fixedProjectId,
}: {
  embedded?: boolean
  fixedHostId?: string
  fixedProjectId?: string
}) {
  const [filters, setFilters] = useState<DockerFilterState>({
    page: 1,
    pageSize: embedded ? 5 : 15,
    hostId: fixedHostId,
    projectId: fixedProjectId,
  })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<DockerPortFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<DockerPortMapping | null>(null)
  const { dockerModuleEnabled, canCreatePorts, canUpdatePorts, canDeletePorts, canViewServices } =
    useDockerPermissions()
  const { localeCode } = useI18n()
  const { hostOptions, projectOptions, serviceOptions } = useDockerOptions({
    includeServices: canViewServices,
  })
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const portsQuery = useQuery(dockerQueries.ports(filters, dockerModuleEnabled))
  const saveMutation = useMutation({
    mutationFn: (values: DockerPortFormValues) =>
      editing
        ? dockerApi.updatePort(editing.id, buildPortPayload(values))
        : dockerApi.createPort(buildPortPayload(values)),
    onSuccess: () => {
      message.success(
        editing
          ? localeText(localeCode, '端口映射已更新', 'Port mapping updated')
          : localeText(localeCode, '端口映射已创建', 'Port mapping created'),
      )
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: dockerApi.deletePort,
    onSuccess: () => {
      message.success(localeText(localeCode, '端口映射已删除', 'Port mapping deleted'))
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(
    readableQueryData(portsQuery),
    filters.page ?? 1,
    filters.pageSize ?? (embedded ? 5 : 15),
  )
  const columns: ColumnsType<DockerPortMapping> = [
    {
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      fixed: 'left',
      width: 180,
      render: (value) => <Text strong>{value}</Text>,
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      width: 105,
      render: (value) => statusTag(value, localeCode),
    },
    {
      title: localeText(localeCode, '映射', 'Mapping'),
      width: 220,
      render: (_value, record) => formatPort(record),
    },
    {
      title: localeText(localeCode, '域名', 'Domain'),
      dataIndex: 'domainName',
      width: 220,
      render: (value, record) =>
        value ? (
          <Space>
            <Text>{value}</Text>
            {record.domainTlsEnabled ? <Tag color="green">TLS</Tag> : null}
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: localeText(localeCode, '暴露范围', 'Exposure'),
      dataIndex: 'exposureScope',
      width: 110,
      render: (value) => {
        const scope = value || 'internal'
        return scope === 'internal'
          ? localeText(localeCode, '内部', 'Internal')
          : scope === 'vpn'
            ? 'VPN'
            : scope === 'public'
              ? localeText(localeCode, '公网', 'Public')
              : scope
      },
    },
    {
      title: localeText(localeCode, '访问地址', 'Access URL'),
      width: 250,
      render: (_value, record) => {
        const url = formatAccessURL(record)
        return url ? (
          <Typography.Link href={url} target="_blank">
            {url}
          </Typography.Link>
        ) : (
          '-'
        )
      },
    },
    {
      title: localeText(localeCode, '主机', 'Host'),
      dataIndex: 'hostId',
      width: 170,
      render: (value) => hostOptions.find((item) => item.value === value)?.label || value,
    },
    {
      title: localeText(localeCode, '项目/服务', 'Project / Service'),
      width: 190,
      render: (_value, record) =>
        [
          projectOptions.find((item) => item.value === record.projectId)?.label || record.projectId,
          serviceOptions.find((item) => item.value === record.serviceId)?.label || record.serviceId,
        ]
          .filter(Boolean)
          .join(' / ') || '-',
    },
    {
      title: localeText(localeCode, '负责人', 'Owner'),
      dataIndex: 'owner',
      width: 120,
      render: (value) => value || '-',
    },
    {
      title: localeText(localeCode, '到期', 'Expires at'),
      dataIndex: 'expiresAt',
      width: 155,
      render: formatDateTime,
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      align: 'center',
      className: 'soha-table-actions-column',
      fixed: 'right',
      width: 96,
      render: (_value, record) =>
        canUpdatePorts || canDeletePorts ? (
          <Space className="soha-row-action-icons">
            {canUpdatePorts ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '编辑端口映射', 'Edit port mapping')}
                size="small"
                tooltip={localeText(localeCode, '编辑', 'Edit')}
                icon={<EditOutlined />}
                onClick={() => {
                  setEditing(record)
                  form.setFieldsValue(record)
                  setDrawerOpen(true)
                }}
              />
            ) : null}
            {canDeletePorts ? (
              <Popconfirm
                title={localeText(localeCode, '确认删除端口映射？', 'Delete this port mapping?')}
                onConfirm={() => deleteMutation.mutate(record.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '删除端口映射', 'Delete port mapping')}
                  size="small"
                  tooltip={localeText(localeCode, '删除', 'Delete')}
                  danger
                  icon={<DeleteOutlined />}
                />
              </Popconfirm>
            ) : null}
          </Space>
        ) : null,
    },
  ]
  return (
    <>
      <ReadFeedback
        query={portsQuery}
        label={localeText(localeCode, '端口映射', 'Port mappings')}
      />
      <ComputeMutationFeedback
        operations={[
          {
            label: localeText(localeCode, '保存端口映射', 'Save port mapping'),
            mutation: saveMutation,
            target: saveMutation.variables?.name,
            receipt: {
              message: localeText(localeCode, '端口映射记录已保存', 'Port mapping saved'),
            },
          },
          {
            label: localeText(localeCode, '删除端口映射', 'Delete port mapping'),
            mutation: deleteMutation,
            target: deleteMutation.variables,
            receipt: {
              message: localeText(localeCode, '端口映射记录已删除', 'Port mapping deleted'),
            },
          },
        ]}
      />
      {!embedded && !fixedProjectId ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={
              <ManagementQueryActions
                loading={portsQuery.isFetching}
                onReset={() => {
                  filterForm.resetFields()
                  setFilters({
                    page: 1,
                    pageSize: filters.pageSize ?? (embedded ? 5 : 15),
                    hostId: fixedHostId,
                    projectId: fixedProjectId,
                  })
                }}
              />
            }
            onFinish={(values) =>
              setFilters((current) => ({
                ...current,
                ...values,
                hostId: fixedHostId,
                projectId: fixedProjectId,
                page: 1,
              }))
            }
          >
            <ManagementKeywordField
              placeholder={localeText(
                localeCode,
                '名称、访问地址或负责人',
                'Name, access URL, or owner',
              )}
            />
            {!fixedHostId ? (
              <ManagementQueryField
                minWidth={180}
                width={220}
                name="hostId"
                label={localeText(localeCode, '主机', 'Host')}
              >
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  placeholder={localeText(localeCode, '全部主机', 'All hosts')}
                  options={hostOptions}
                />
              </ManagementQueryField>
            ) : null}
            {!fixedProjectId ? (
              <ManagementQueryField
                minWidth={180}
                width={220}
                name="projectId"
                label={localeText(localeCode, '项目', 'Project')}
              >
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  placeholder={localeText(localeCode, '全部项目', 'All projects')}
                  options={projectOptions}
                />
              </ManagementQueryField>
            ) : null}
            <ManagementQueryField
              minWidth={132}
              width={150}
              name="status"
              label={localeText(localeCode, '状态', 'Status')}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部', 'All')}
                options={['active', 'reserved', 'released', 'expired'].map((item) => ({
                  value: item,
                  label: formatStatusLabel(item, localeCode),
                }))}
              />
            </ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={portsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 1470 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        actions={
          canCreatePorts && !embedded ? (
            <>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => {
                  setEditing(null)
                  form.setFieldsValue({
                    hostId: fixedHostId,
                    projectId: fixedProjectId,
                    protocol: 'tcp',
                    exposureScope: 'internal',
                    status: 'active',
                    domainScheme: 'http',
                    domainTlsEnabled: false,
                  })
                  setDrawerOpen(true)
                }}
              >
                {localeText(localeCode, '新增映射', 'Add mapping')}
              </Button>
            </>
          ) : null
        }
        enableDensity={!embedded}
        refreshing={portsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => portsQuery.refetch()}
      />
      <Modal
        title={
          editing
            ? localeText(localeCode, '编辑端口映射', 'Edit port mapping')
            : localeText(localeCode, '新增端口映射', 'Add port mapping')
        }
        open={drawerOpen}
        onCancel={() => setDrawerOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={saveMutation.isPending}
        okText={localeText(localeCode, '保存', 'Save')}
        cancelText={localeText(localeCode, '取消', 'Cancel')}
        width={720}
        destroyOnHidden
        mask={{ closable: false }}
        style={{ top: 32 }}
        styles={{ body: { ...scrollableModalBodyStyle, maxHeight: 'calc(100dvh - 180px)' } }}
      >
        <ComputeOperationFeedback
          label={localeText(localeCode, '保存端口映射', 'Save port mapping')}
          pending={saveMutation.isPending}
          error={saveMutation.error}
        />
        <Form<DockerPortFormValues>
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={
            editing ?? {
              hostId: fixedHostId,
              projectId: fixedProjectId,
              protocol: 'tcp',
              exposureScope: 'internal',
              status: 'active',
              domainScheme: 'http',
              domainTlsEnabled: false,
            }
          }
          onFinish={(values) => saveMutation.mutate(values)}
        >
          <Form.Item
            name="name"
            label={localeText(localeCode, '名称', 'Name')}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <div className="soha-docker-form-grid">
            <Form.Item
              name="hostId"
              label={localeText(localeCode, 'Docker 主机', 'Docker host')}
              rules={[{ required: true }]}
              hidden={Boolean(fixedHostId)}
            >
              <Select showSearch={{ optionFilterProp: 'label' }} options={hostOptions} />
            </Form.Item>
            <Form.Item name="hostIp" label={localeText(localeCode, '监听 IP', 'Listen IP')}>
              <Input placeholder="0.0.0.0" />
            </Form.Item>
            <Form.Item
              name="hostPort"
              label={localeText(localeCode, '主机端口', 'Host port')}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="containerPort"
              label={localeText(localeCode, '容器端口', 'Container port')}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="protocol" label={localeText(localeCode, '协议', 'Protocol')}>
              <Select
                options={[
                  { value: 'tcp', label: 'tcp' },
                  { value: 'udp', label: 'udp' },
                ]}
              />
            </Form.Item>
            <Form.Item name="exposureScope" label={localeText(localeCode, '暴露范围', 'Exposure')}>
              <Select
                options={['internal', 'vpn', 'public'].map((item) => ({
                  value: item,
                  label:
                    item === 'internal'
                      ? localeText(localeCode, '内部', 'Internal')
                      : item === 'vpn'
                        ? 'VPN'
                        : localeText(localeCode, '公网', 'Public'),
                }))}
              />
            </Form.Item>
            <Form.Item name="status" label={localeText(localeCode, '状态', 'Status')}>
              <Select
                options={['active', 'reserved', 'released', 'expired'].map((item) => ({
                  value: item,
                  label: formatStatusLabel(item, localeCode),
                }))}
              />
            </Form.Item>
            <Form.Item name="owner" label={localeText(localeCode, '负责人', 'Owner')}>
              <Input />
            </Form.Item>
            <Form.Item
              name="projectId"
              label={localeText(localeCode, '项目', 'Project')}
              hidden={Boolean(fixedProjectId)}
            >
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                options={projectOptions}
              />
            </Form.Item>
            <Form.Item name="serviceId" label={localeText(localeCode, '服务', 'Service')}>
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                options={serviceOptions}
              />
            </Form.Item>
          </div>

          <div className="soha-docker-form-grid is-access">
            <Form.Item name="domainName" label={localeText(localeCode, '访问域名', 'Domain name')}>
              <Input placeholder="preview.internal.example.com" />
            </Form.Item>
            <Form.Item name="domainScheme" label={localeText(localeCode, '域名协议', 'Scheme')}>
              <Select
                options={[
                  { value: 'http', label: 'http' },
                  { value: 'https', label: 'https' },
                ]}
              />
            </Form.Item>
            <Form.Item name="domainTlsEnabled" label="TLS" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
          <Form.Item name="accessUrl" label={localeText(localeCode, '访问地址', 'Access URL')}>
            <Input placeholder="http://10.0.0.10:8080" />
          </Form.Item>
          <Form.Item name="expiresAt" label={localeText(localeCode, '到期时间', 'Expires at')}>
            <Input placeholder="2026-06-01T10:00:00Z" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
