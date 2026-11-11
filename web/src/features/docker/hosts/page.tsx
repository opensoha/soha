import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { App, Button, Descriptions, Form, Input, Popconfirm, Select, Space } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementDataPage } from '@/components/management-data-page'
import { MetadataTag } from '@/components/status-tag'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { computeQueries, latestTaskForResource, ResourceTaskActions } from '@/features/compute'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import type { DockerHost, DockerQuickCreateHostInput } from '../docker-types'
import { RuntimeHostModal } from './create-page'
import {
  ARCHITECTURE_OPTIONS,
  DockerAdminTable,
  HOST_STATUS_OPTIONS,
  architectureTag,
  compactRecord,
  formatBytes,
  normalizePage,
  pageTablePagination,
  refreshDocker,
  statusTag,
  type DockerFilterState,
  useDockerPermissions,
} from '../shared/ui'

interface QuickCreateHostFormValues extends DockerQuickCreateHostInput {
  memoryGiB?: number
  diskGiB?: number
}

export function buildQuickHostPayload(
  values: QuickCreateHostFormValues,
): DockerQuickCreateHostInput {
  return compactRecord({
    ...values,
    memoryBytes: values.memoryGiB ? Math.round(values.memoryGiB * 1024 ** 3) : values.memoryBytes,
    diskBytes: values.diskGiB ? Math.round(values.diskGiB * 1024 ** 3) : values.diskBytes,
    memoryGiB: undefined,
    diskGiB: undefined,
  })
}

function HostsTable({ embedded = false }: { embedded?: boolean }) {
  const [filters, setFilters] = useState<DockerFilterState>({
    page: 1,
    pageSize: embedded ? 5 : 15,
  })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<DockerHost | null>(null)
  const { dockerModuleEnabled, canCreateHosts, canUpdateHosts, canDeleteHosts, canViewOperations } =
    useDockerPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const hostsQuery = useQuery(dockerQueries.hosts(filters, dockerModuleEnabled))
  const tasksQuery = useQuery({
    ...computeQueries.tasks({ domain: 'container_runtime', limit: 100 }),
    enabled: dockerModuleEnabled && canViewOperations,
  })
  const deleteMutation = useMutation({
    mutationFn: dockerApi.deleteHost,
    onSuccess: () => {
      message.success(localeText(localeCode, '主机已删除', 'Host deleted'))
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(
    readableQueryData(hostsQuery),
    filters.page ?? 1,
    filters.pageSize ?? (embedded ? 5 : 15),
  )
  const columns: ColumnsType<DockerHost> = [
    {
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      fixed: 'left',
      width: 190,
      render: (value, record) => (
        <Link to={`/compute/runtimes/hosts/${encodeURIComponent(record.id)}`}>
          {value || record.id}
        </Link>
      ),
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      width: 110,
      render: statusTag,
    },
    {
      title: localeText(localeCode, '架构', 'Architecture'),
      dataIndex: 'architecture',
      width: 120,
      render: architectureTag,
    },
    {
      title: 'Endpoint',
      dataIndex: 'endpoint',
      width: 220,
      render: (value, record) => value || record.ipAddress || '-',
    },
    {
      title: 'VM',
      width: 180,
      render: (_value, record) =>
        record.vmName || record.vmId || record.virtualizationConnectionId || '-',
    },
    {
      title: localeText(localeCode, '规格', 'Resources'),
      width: 180,
      render: (_value, record) => {
        const values = [
          record.cpuCoreCount ? (
            <MetadataTag key="cpu" label={`${record.cpuCoreCount} CPU`} tone="purple" />
          ) : null,
          record.memoryBytes ? (
            <MetadataTag key="memory" label={formatBytes(record.memoryBytes)} tone="cyan" />
          ) : null,
          record.diskBytes ? (
            <MetadataTag key="disk" label={formatBytes(record.diskBytes)} tone="gold" />
          ) : null,
        ].filter(Boolean)
        return values.length > 0 ? <Space size={4}>{values}</Space> : '-'
      },
    },
    {
      title: localeText(localeCode, '心跳', 'Heartbeat'),
      dataIndex: 'lastHeartbeatAt',
      width: 155,
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.task,
      title: localeText(localeCode, '最近任务', 'Latest task'),
      render: (_value, record) => (
        <ResourceTaskActions
          task={latestTaskForResource(
            canViewOperations ? (readableQueryData(tasksQuery)?.items ?? []) : [],
            'runtime_host',
            record.id,
          )}
          resourceKind="runtime_host"
          resourceId={record.id}
        />
      ),
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      align: 'center',
      className: 'soha-table-actions-column',
      fixed: 'right',
      width: 96,
      render: (_value, record) =>
        canUpdateHosts || canDeleteHosts ? (
          <Space className="soha-row-action-icons">
            {canUpdateHosts ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '编辑主机', 'Edit host')}
                size="small"
                tooltip={localeText(localeCode, '编辑', 'Edit')}
                icon={<EditOutlined />}
                onClick={() => {
                  setEditing(record)
                  setEditorOpen(true)
                }}
              />
            ) : null}
            {canDeleteHosts ? (
              <Popconfirm
                title={localeText(localeCode, '确认删除 Docker 主机？', 'Delete this Docker host?')}
                onConfirm={() => deleteMutation.mutate(record.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '删除主机', 'Delete host')}
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
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={
              <ManagementQueryActions
                loading={hostsQuery.isFetching}
                onReset={() => {
                  filterForm.resetFields()
                  setFilters({ page: 1, pageSize: filters.pageSize ?? (embedded ? 5 : 15) })
                }}
              />
            }
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
          >
            <ManagementKeywordField
              placeholder={localeText(
                localeCode,
                '主机、Endpoint、VM 或 IP',
                'Host, endpoint, VM, or IP',
              )}
            />
            <ManagementQueryField
              minWidth={132}
              width={150}
              name="status"
              label={localeText(localeCode, '状态', 'Status')}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部', 'All')}
                options={HOST_STATUS_OPTIONS.map((item) => ({
                  value: item,
                  label: formatStatusLabel(item, localeCode),
                }))}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={148}
              width={170}
              name="architecture"
              label={localeText(localeCode, '架构', 'Architecture')}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部', 'All')}
                options={ARCHITECTURE_OPTIONS}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={150}
              width={180}
              name="environment"
              label={localeText(localeCode, '环境', 'Environment')}
            >
              <Input allowClear placeholder="dev / test" />
            </ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <ReadFeedback
        query={hostsQuery}
        label={localeText(localeCode, '运行时主机', 'Runtime hosts')}
      />
      <ReadFeedback query={tasksQuery} label={localeText(localeCode, '主机任务', 'Host tasks')} />
      <ComputeMutationFeedback
        operations={[
          {
            label: localeText(localeCode, '删除主机记录', 'Delete host record'),
            mutation: deleteMutation,
            target: deleteMutation.variables,
            receipt: { message: localeText(localeCode, '主机记录已删除', 'Host record deleted') },
          },
        ]}
      />
      <DockerAdminTable
        empty={
          hostsQuery.isError
            ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
            : undefined
        }
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={hostsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        pagination={pageTablePagination(page, embedded, setFilters)}
        expandable={{
          expandedRowRender: (record: DockerHost) => (
            <Descriptions
              size="small"
              bordered
              column={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 2, xxl: 2 }}
            >
              <Descriptions.Item label={localeText(localeCode, '环境', 'Environment')}>
                {record.environment || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '归属', 'Owner')}>
                {record.owner || record.team || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="VM ID">{record.vmId || '-'}</Descriptions.Item>
              <Descriptions.Item
                label={localeText(localeCode, '虚拟化连接', 'Virtualization connection')}
              >
                {record.virtualizationConnectionId || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '端口池', 'Port pool')}>
                {record.availablePortStart && record.availablePortEnd
                  ? `${record.availablePortStart}-${record.availablePortEnd}`
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Agent">{record.agentVersion || '-'}</Descriptions.Item>
              <Descriptions.Item label="Docker">{record.dockerVersion || '-'}</Descriptions.Item>
              <Descriptions.Item label="Compose">{record.composeVersion || '-'}</Descriptions.Item>
            </Descriptions>
          ),
        }}
        actions={
          canCreateHosts && !embedded ? (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(null)
                setEditorOpen(true)
              }}
            >
              {localeText(localeCode, '新增主机', 'Add host')}
            </Button>
          ) : null
        }
        enableDensity={!embedded}
        refreshing={hostsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => hostsQuery.refetch()}
      />
      <RuntimeHostModal
        editing={editing}
        open={editorOpen}
        onClose={() => {
          setEditorOpen(false)
          setEditing(null)
        }}
      />
    </>
  )
}

export function DockerHostsPage() {
  return <ManagementDataPage className="soha-docker-page" tableNode={<HostsTable />} />
}
