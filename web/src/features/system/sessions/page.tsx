import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementRefreshButton,
  ManagementTableToolbar,
  useManagementTextFilter,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { loginProviderLabel, loginProviderTagColor } from '@/utils/login-provider'
import { queryAccessDenied, readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import { DeleteOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Popconfirm, Select } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { systemMutations } from '../mutations'
import { systemQueries } from '../queries'
import type { OnlineUser } from '../system-model'

function SourceTag({ value }: { value?: string }) {
  const label = (value || '').trim()
  return label ? <MetadataTag label={label.toUpperCase()} tone="blue" /> : <>-</>
}

export function OnlineUsersPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [selectedSessionIds, setSelectedSessionIds] = useState<string[]>([])
  const [providerFilter, setProviderFilter] = useState<string>('')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const canManageOnlineUsers = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'system.online-users.revoke',
  )

  const sessionsQuery = useQuery(systemQueries.sessions())
  const sessions = readableQueryData(sessionsQuery) ?? []
  const { isFetching, isLoading, refetch } = sessionsQuery
  const revokeMutation = useOperationMutation(systemMutations.sessions.revoke(queryClient), {
    label: ['下线会话', 'Revoke session'],
    target: (id) => id,
  })
  const batchRevokeMutation = useOperationMutation(
    systemMutations.sessions.revokeMany(queryClient),
    {
      label: ['批量下线会话', 'Revoke sessions'],
      target: (ids) => ids.join('、'),
      resultError: (results) => {
        const failed = results.filter((r) => r.status === 'rejected')
        return (
          failed.find((r) => writeOutcomeUnknown(r.reason))?.reason ||
          (failed.length
            ? new Error(`部分会话下线失败：${failed.length} 个，请核对保留的选中项。`)
            : undefined)
        )
      },
      receipt: (results) => ({
        message: '批量下线请求已返回',
        evidence: `成功 ${results.filter((r) => r.status === 'fulfilled').length}，失败 ${results.filter((r) => r.status === 'rejected').length}；失败目标保留在选中项中。`,
      }),
    },
  )
  const blocked =
    revokeMutation.blocked || batchRevokeMutation.blocked || queryAccessDenied(sessionsQuery)
  useEffect(() => {
    if (queryAccessDenied(sessionsQuery)) setSelectedSessionIds([])
  }, [sessionsQuery.error])
  const providerOptions = useMemo(
    () =>
      Array.from(new Set(sessions.map((item: OnlineUser) => item.providerType).filter(Boolean)))
        .sort()
        .map((value) => ({
          value,
          label: value,
        })),
    [sessions],
  )
  const keywordFilteredSessions = useManagementTextFilter(
    sessions,
    searchKeyword,
    (item: OnlineUser) => [
      item.userId,
      item.userName,
      item.email,
      item.providerType,
      item.source,
      item.sourceIp,
      item.userAgent,
    ],
  )
  const filteredSessions = useMemo(() => {
    return keywordFilteredSessions.filter(
      (item: OnlineUser) => !providerFilter || item.providerType === providerFilter,
    )
  }, [keywordFilteredSessions, providerFilter])
  const selectedSessions = useMemo(
    () => filteredSessions.filter((item: OnlineUser) => selectedSessionIds.includes(item.id)),
    [filteredSessions, selectedSessionIds],
  )

  const columns: TableColumnsType<OnlineUser> = [
    { title: '用户 ID', dataIndex: 'userId', width: 180, ellipsis: true },
    { title: '用户名', dataIndex: 'userName', width: 140 },
    { title: '邮箱', dataIndex: 'email', width: 240, ellipsis: true },
    {
      title: '登录方式',
      dataIndex: 'providerType',
      width: 120,
      render: (value: string) =>
        value ? (
          <MetadataTag label={loginProviderLabel(value)} tone={loginProviderTagColor(value)} />
        ) : (
          '-'
        ),
    },
    {
      title: '来源',
      dataIndex: 'source',
      width: 100,
      render: (value: string) => <SourceTag value={value} />,
    },
    {
      title: 'IP',
      dataIndex: 'sourceIp',
      width: 140,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: '设备',
      dataIndex: 'userAgent',
      width: 280,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '登录时间',
      dataIndex: 'loginTime',
      render: (_: string, record: OnlineUser) => formatDateTime(record.loginTime),
    },
    {
      ...tableColumnPresets.datetime,
      title: '最近活跃',
      dataIndex: 'lastSeenAt',
      render: (_: string, record: OnlineUser) => formatDateTime(record.lastSeenAt),
    },
    {
      title: '活跃时长',
      dataIndex: 'lastSeenAt',
      width: 120,
      render: (_: string, record: OnlineUser) => formatRelativeTime(record.lastSeenAt),
    },
    {
      ...tableColumnPresets.datetime,
      title: '过期时间',
      dataIndex: 'expiry',
      render: (_: string, record: OnlineUser) => formatDateTime(record.expiry),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 88,
      dataIndex: 'id',
      render: (_: string, record: OnlineUser) =>
        canManageOnlineUsers ? (
          <Popconfirm
            title="确认下线该用户会话？"
            disabled={blocked}
            okButtonProps={{ disabled: blocked }}
            onConfirm={() =>
              revokeMutation.mutate(record.id, {
                onSuccess: () => {
                  setSelectedSessionIds((current) => current.filter((id) => id !== record.id))
                },
              })
            }
          >
            <ManagementIconButton
              size="small"
              danger
              icon={<DeleteOutlined />}
              aria-label="下线用户"
              disabled={blocked}
              loading={revokeMutation.isPending}
              tooltip="下线用户"
            />
          </Popconfirm>
        ) : (
          '-'
        ),
    },
  ]

  return (
    <ManagementDataPage
      beforeQuery={
        <div className="soha-page-feedback">
          {!queryAccessDenied(sessionsQuery) ? (
            <>
              {revokeMutation.feedback}
              {batchRevokeMutation.feedback}
            </>
          ) : null}
        </div>
      }
      query={{
        onFinish: () => undefined,
        actions: (
          <ManagementQueryActions
            disabledReset={!providerFilter && !searchKeyword.trim()}
            onReset={() => {
              setProviderFilter('')
              setSearchKeyword('')
            }}
          />
        ),
        children: (
          <>
            <ManagementQueryField minWidth={160} width={180} label="登录方式">
              <Select
                allowClear
                placeholder="全部方式"
                value={providerFilter || undefined}
                onChange={(value) => setProviderFilter(value || '')}
                options={providerOptions}
              />
            </ManagementQueryField>
            <ManagementKeywordField
              label="关键字"
              placeholder="搜索用户 / 邮箱 / IP / 设备"
              value={searchKeyword}
              onChange={setSearchKeyword}
            />
          </>
        ),
      }}
      table={{
        columnSettingIconOnly: true,
        columnSettingPlacement: 'header',
        headerExtra: (
          <ManagementTableToolbar>
            {canManageOnlineUsers ? (
              <Button
                size="small"
                danger
                variant="outlined"
                disabled={blocked || selectedSessions.length === 0}
                loading={batchRevokeMutation.isPending}
                onClick={() =>
                  batchRevokeMutation.mutate(
                    selectedSessions.map((item: OnlineUser) => item.id),
                    {
                      onSuccess: (results, submitted) => {
                        setSelectedSessionIds(
                          submitted.filter((_, index) => results[index]?.status === 'rejected'),
                        )
                      },
                    },
                  )
                }
              >
                {`批量下线 (${selectedSessions.length})`}
              </Button>
            ) : null}
            <ManagementDensityButton
              aria-label="切换在线用户表格密度"
              size="small"
              tooltip={tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度'}
              onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label="刷新在线用户"
              loading={isFetching}
              size="small"
              tooltip="刷新"
              onClick={() => void refetch()}
            />
          </ManagementTableToolbar>
        ),
        error: sessionsQuery.error,
        hasReadData: readableQueryData(sessionsQuery) !== undefined,
        onRetry: () => void refetch(),
        columns,
        dataSource: filteredSessions,
        rowKey: 'id',
        loading: isLoading,
        pageSize: 20,
        scroll: { x: 'max-content' },
        tableSize,
        rowSelection: canManageOnlineUsers
          ? {
              selectedRowKeys: selectedSessionIds,
              onChange: (selectedRowKeys: React.Key[]) =>
                setSelectedSessionIds(selectedRowKeys.map(String)),
            }
          : undefined,
      }}
    />
  )
}
