import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { OperationFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Popconfirm, Space, Switch, Typography } from 'antd'
import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { systemIntegrationMutations } from './mutations'
import { systemIntegrationQueries } from './queries'
import './styles.css'
import type { SystemIntegration } from './types'

const { Text } = Typography

export function SourceConnectionsPage() {
  const location = useLocation()
  const savedSource = location.state?.savedSource as string | undefined
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const permissionQuery = usePermissionSnapshot()
  const permissionSnapshot = readableQueryData(permissionQuery)?.data
  const canView = hasPermission(permissionSnapshot, 'settings.system-integrations.view')
  const canCreate = hasPermission(permissionSnapshot, 'settings.system-integrations.create')
  const canUpdate = hasPermission(permissionSnapshot, 'settings.system-integrations.update')
  const canDelete = hasPermission(permissionSnapshot, 'settings.system-integrations.delete')
  const canTest = hasPermission(permissionSnapshot, 'settings.system-integrations.test')
  const listQuery = useQuery(systemIntegrationQueries.list({ category: 'source_control' }, canView))
  const updateMutation = useOperationMutation(systemIntegrationMutations.update(queryClient), {
    label: ['更新代码源连接', 'Update source connection'],
    target: (v) => v.id,
  })
  const testMutation = useOperationMutation(systemIntegrationMutations.test(queryClient), {
    label: ['测试代码源连接', 'Test source connection'],
    target: (id) => id,
    readOnly: true,
    resultError: (r) =>
      r.status === 'failed' ? new Error(r.message || '连接测试失败') : undefined,
    receipt: (r) => ({ message: r.message || '连接测试已返回' }),
  })
  const deleteMutation = useOperationMutation(systemIntegrationMutations.remove(queryClient), {
    label: ['删除代码源连接', 'Delete source connection'],
    target: (id) => id,
  })
  const records = canView ? readableQueryData(listQuery) : undefined
  const blocked =
    updateMutation.blocked ||
    testMutation.blocked ||
    deleteMutation.blocked ||
    queryAccessDenied(listQuery)

  if (!permissionQuery.isLoading && !canView) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看代码源连接的权限。" />
      </div>
    )
  }

  const columns: TableColumnsType<SystemIntegration> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value, row) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{value}</Text>
          <Text type="secondary">{row.description || row.providerType}</Text>
        </Space>
      ),
    },
    {
      title: '类型',
      dataIndex: 'providerType',
      width: 120,
      render: (value: string) => <MetadataTag label={value === 'gitlab' ? 'GitLab' : value} />,
    },
    {
      title: '健康状态',
      dataIndex: 'healthStatus',
      width: 120,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '最近检查',
      dataIndex: 'lastCheckedAt',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 90,
      render: (enabled: boolean, row) => (
        <Switch
          aria-label={`启用 ${row.name}`}
          checked={enabled}
          disabled={!canUpdate || blocked}
          loading={updateMutation.isPending}
          size="small"
          onChange={(checked) =>
            updateMutation.mutate({
              id: row.id,
              values: { expectedVersion: row.version, enabled: checked },
            })
          }
        />
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 152,
      className: 'soha-table-actions-column',
      align: 'right',
      render: (_, row) => (
        <Space className="soha-row-action-icons" size={4}>
          <ManagementIconButton
            aria-label={`测试 ${row.name}`}
            disabled={!canTest || !row.enabled || blocked}
            icon={<ThunderboltOutlined />}
            loading={testMutation.isPending && testMutation.variables === row.id}
            tooltip="测试连接"
            onClick={() => testMutation.mutate(row.id)}
          />
          <ManagementIconButton
            aria-label={`编辑 ${row.name}`}
            icon={<EditOutlined />}
            tooltip="编辑"
            onClick={() => navigate(`/settings/source-control/${row.id}`)}
          />
          {canDelete ? (
            <Popconfirm
              title={`删除代码源连接 ${row.name}？`}
              description="删除后使用此连接的代码源访问将不可用。"
              okText="删除"
              cancelText="取消"
              disabled={blocked}
              okButtonProps={{ danger: true, disabled: blocked }}
              onConfirm={() => deleteMutation.mutate(row.id)}
            >
              <ManagementIconButton
                aria-label={`删除 ${row.name}`}
                disabled={blocked}
                danger
                icon={<DeleteOutlined />}
                loading={deleteMutation.isPending && deleteMutation.variables === row.id}
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <ManagementDataPage
      beforeQuery={
        <div className="soha-page-feedback">
          {canView && !queryAccessDenied(listQuery) ? (
            <>
              {savedSource ? (
                <OperationFeedback
                  label="保存代码源连接"
                  target={savedSource}
                  receipt={{
                    message: '代码源连接已保存',
                    evidence: '运行健康状态请通过测试连接核对。',
                  }}
                />
              ) : null}
              {updateMutation.feedback}
              {testMutation.feedback}
              {deleteMutation.feedback}
            </>
          ) : null}
        </div>
      }
      className="soha-system-integrations-page"
      table={{
        columns,
        dataSource: records ?? [],
        error: canView ? listQuery.error : null,
        hasReadData: records !== undefined,
        onRetry: canView ? () => void listQuery.refetch() : undefined,
        empty: (
          <ManagementState
            bordered={false}
            compact
            title="暂无代码源连接"
            description="新增 Git 连接后，交付、虚拟化等工作台可通过统一代码源接口访问仓库文件。"
          />
        ),
        columnSettingIconOnly: true,
        toolbarExtra: (
          <ManagementTableToolbar>
            {canCreate ? (
              <Button
                icon={<PlusOutlined />}
                size="small"
                type="primary"
                onClick={() => navigate('/settings/source-control/new')}
              >
                新增 Git
              </Button>
            ) : null}
            <ManagementDensityButton
              aria-label="切换表格密度"
              tooltip={tableSize === 'small' ? '切换为舒展密度' : '切换为紧凑密度'}
              onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label="刷新代码源连接"
              loading={listQuery.isFetching}
              tooltip="刷新"
              onClick={() => void listQuery.refetch()}
            />
          </ManagementTableToolbar>
        ),
        loading: listQuery.isLoading,
        paginationSummary: (total, range) => (
          <Text type="secondary">
            当前 {range[0]}-{range[1]} / {total} 条
          </Text>
        ),
        rowKey: 'id',
        tableSize,
      }}
    />
  )
}
