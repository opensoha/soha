import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import { useState } from 'react'
import { App, Drawer, Form, Select, Space, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  FileTextOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { formatDateTime } from '@/utils/time'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import { DockerProjectLogsPanel } from '../runtime/logs-panel'
import type { DockerService } from '../docker-types'
import {
  DockerAdminTable,
  formatBytes,
  formatPercent,
  normalizePage,
  operationActionLabel,
  pageTablePagination,
  refreshDocker,
  statusTag,
  type DockerFilterState,
  useDockerOptions,
  useDockerPermissions,
} from '../shared/ui'

const { Text } = Typography

export function ServicesTable({
  embedded = false,
  fixedProjectId,
}: {
  embedded?: boolean
  fixedProjectId?: string
}) {
  const [filters, setFilters] = useState<DockerFilterState>({
    page: 1,
    pageSize: embedded ? 5 : 15,
    projectId: fixedProjectId,
  })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [logService, setLogService] = useState<DockerService | null>(null)
  const {
    dockerModuleEnabled,
    canStartServices,
    canStopServices,
    canRestartServices,
    canViewServiceLogs,
    canViewOperations,
  } = useDockerPermissions()
  const { hostOptions, projectOptions } = useDockerOptions({ includeServices: false })
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const servicesQuery = useQuery(dockerQueries.services(filters, dockerModuleEnabled))
  const actionMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) =>
      dockerApi.serviceAction(id, action),
    onSuccess: (_response, variables) => {
      message.success(
        localeText(
          localeCode,
          `${operationActionLabel(variables.action, localeCode)}任务已提交`,
          `${operationActionLabel(variables.action, localeCode)} task submitted`,
        ),
      )
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(
    readableQueryData(servicesQuery),
    filters.page ?? 1,
    filters.pageSize ?? (embedded ? 5 : 15),
  )
  const serviceActions = [
    { action: 'restart', allowed: canRestartServices, icon: <ReloadOutlined /> },
    { action: 'start', allowed: canStartServices, icon: <PlayCircleOutlined /> },
    { action: 'stop', allowed: canStopServices, icon: <PoweroffOutlined /> },
  ].filter((item) => item.allowed)
  const columns: ColumnsType<DockerService> = [
    {
      title: localeText(localeCode, '服务', 'Service'),
      dataIndex: 'name',
      fixed: 'left',
      width: 180,
      render: (value, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{value}</Text>
          <Text type="secondary">{record.containerId || record.id}</Text>
        </Space>
      ),
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      width: 110,
      render: (value) => statusTag(value, localeCode),
    },
    {
      title: localeText(localeCode, '镜像', 'Image'),
      dataIndex: 'image',
      width: 240,
      render: (value) => value || '-',
    },
    {
      title: localeText(localeCode, '项目', 'Project'),
      dataIndex: 'projectId',
      width: 180,
      render: (value) => projectOptions.find((item) => item.value === value)?.label || value,
    },
    {
      title: localeText(localeCode, '主机', 'Host'),
      dataIndex: 'hostId',
      width: 170,
      render: (value) => hostOptions.find((item) => item.value === value)?.label || value,
    },
    { title: 'CPU', dataIndex: 'cpuPercent', width: 90, render: formatPercent },
    {
      title: localeText(localeCode, '内存', 'Memory'),
      dataIndex: 'memoryBytes',
      width: 110,
      render: formatBytes,
    },
    {
      title: localeText(localeCode, '网络', 'Network'),
      width: 150,
      render: (_value, record) =>
        `${formatBytes(record.networkRxBytes)} / ${formatBytes(record.networkTxBytes)}`,
    },
    { title: localeText(localeCode, '重启', 'Restarts'), dataIndex: 'restartCount', width: 80 },
    {
      title: localeText(localeCode, '最近同步', 'Last seen'),
      dataIndex: 'lastSeenAt',
      width: 155,
      render: formatDateTime,
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      align: 'center',
      className: 'soha-table-actions-column',
      fixed: 'right',
      width: 130,
      render: (_value, record) =>
        serviceActions.length > 0 || canViewServiceLogs ? (
          <Space className="soha-row-action-icons">
            {serviceActions.map(({ action, icon }) => (
              <ManagementIconButton
                key={action}
                aria-label={operationActionLabel(action, localeCode)}
                size="small"
                tooltip={operationActionLabel(action, localeCode)}
                icon={icon}
                loading={actionMutation.isPending}
                onClick={() => actionMutation.mutate({ id: record.id, action })}
              />
            ))}
            {canViewServiceLogs ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '查看日志', 'View logs')}
                size="small"
                tooltip={localeText(localeCode, '日志', 'Logs')}
                icon={<FileTextOutlined />}
                onClick={() => setLogService(record)}
              />
            ) : null}
          </Space>
        ) : null,
    },
  ]
  return (
    <>
      <Drawer
        title={localeText(
          localeCode,
          `服务日志 · ${logService?.name ?? ''}`,
          `Service logs · ${logService?.name ?? ''}`,
        )}
        open={logService !== null}
        onClose={() => setLogService(null)}
        size={960}
        destroyOnHidden
      >
        {logService ? (
          <DockerProjectLogsPanel
            enabled={dockerModuleEnabled && canViewServiceLogs}
            projectId={logService.projectId}
            serviceName={logService.name}
            serviceOptions={[{ label: logService.name, value: logService.name }]}
            servicesLoading={false}
            onServiceChange={() => {}}
          />
        ) : null}
      </Drawer>
      {!embedded && !fixedProjectId ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={
              <ManagementQueryActions
                loading={servicesQuery.isFetching}
                onReset={() => {
                  filterForm.resetFields()
                  setFilters({
                    page: 1,
                    pageSize: filters.pageSize ?? (embedded ? 5 : 15),
                    projectId: fixedProjectId,
                  })
                }}
              />
            }
            onFinish={(values) =>
              setFilters((current) => ({
                ...current,
                ...values,
                projectId: fixedProjectId,
                page: 1,
              }))
            }
          >
            <ManagementKeywordField
              placeholder={localeText(
                localeCode,
                '服务、镜像或容器',
                'Service, image, or container',
              )}
            />
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
                options={['defined', 'running', 'exited', 'failed', 'unknown'].map((item) => ({
                  value: item,
                  label: formatStatusLabel(item, localeCode),
                }))}
              />
            </ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <ReadFeedback
        query={servicesQuery}
        label={localeText(localeCode, '容器服务', 'Container services')}
      />
      <ComputeMutationFeedback
        operations={[
          {
            label: localeText(localeCode, '容器服务操作', 'Container service operation'),
            mutation: actionMutation,
            target: actionMutation.variables?.id,
            receipt: {
              message: localeText(
                localeCode,
                '服务操作任务已提交，请核对执行结果',
                'Service task submitted; check the execution result',
              ),
              taskId: actionMutation.data?.id,
              domain: 'container_runtime',
              canViewTask: canViewOperations,
            },
          },
        ]}
      />
      <DockerAdminTable
        empty={
          servicesQuery.isError
            ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
            : undefined
        }
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={servicesQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 1440 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        enableDensity={!embedded}
        refreshing={servicesQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => servicesQuery.refetch()}
      />
    </>
  )
}
