import { MutationFeedback, OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { useRef, useState } from 'react'
import { App, Button, Modal, Space, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import {
  CloudSyncOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ManagementIconButton,
  ManagementState,
  useManagementTextFilter,
} from '@/components/management-list'
import { visuallyHiddenModalTitleStyle } from '@/components/modal-styles'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { settingsQueries } from '@/features/settings'
import { formatDateTime } from '@/utils/time'
import { AccessManagementTablePage } from '../shared/management-page'
import { directorySyncApi } from './api'
import { ChangePreviewModal } from './change-preview'
import { DirectoryConnectionModal } from './connection-drawer'
import { DirectoryConflictCenter } from './conflict-center'
import { directorySyncMutations } from './mutations'
import { directorySyncQueries } from './queries'
import { DirectoryRuntimePanel } from './runtime-status'
import { DirectoryRunHistory } from './run-history'
import type { DirectoryConnection, DirectoryConnectionInput, DirectorySyncPreview } from './types'
import '../shared/styles.css'

const { Text } = Typography
const providerLabels: Record<string, string> = {
  feishu: '飞书',
  wecom: '企业微信',
  dingtalk: '钉钉',
  ldap: 'LDAP',
  scim: 'SCIM',
  custom: '自定义',
}
const modeLabels: Record<string, string> = {
  manual: '手动',
  scheduled: '定时',
  scheduled_and_realtime: '定时 + 实时',
}

export function DirectorySyncPage() {
  const { modal } = App.useApp()
  const queryClient = useQueryClient()
  const permissionQuery = usePermissionSnapshot()
  const snapshot = permissionQuery.data?.data
  const canView = hasPermission(snapshot, 'access.directory.view')
  const canViewLoginSettings = hasPermission(snapshot, 'settings.identity.view')
  const canCreate = hasPermission(snapshot, 'access.directory.create')
  const canUpdate = hasPermission(snapshot, 'access.directory.update')
  const canValidate = hasPermission(snapshot, 'access.directory.validate')
  const canSync = hasPermission(snapshot, 'access.directory.sync')
  const canManagePeople = hasPermission(snapshot, 'access.directory.people.resolve')
  const [search, setSearch] = useState('')
  const [editing, setEditing] = useState<DirectoryConnection | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [selectedId, setSelectedId] = useState('')
  const [runtimeTab, setRuntimeTab] = useState('runtime')
  const [previewConnection, setPreviewConnection] = useState<DirectoryConnection | null>(null)
  const [preview, setPreview] = useState<DirectorySyncPreview | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)

  const connectionsQuery = useQuery(directorySyncQueries.connections(canView))
  const connections = readableQueryData(connectionsQuery) ?? []
  const selected = connections.find((item) => item.id === selectedId)
  const runsQuery = useQuery(directorySyncQueries.runs(selected?.id ?? ''))
  const runtimeStatusQuery = useQuery(directorySyncQueries.runtimeStatus(selected?.id ?? ''))
  const eventsQuery = useQuery(directorySyncQueries.events(selected?.id ?? ''))
  const conflictsQuery = useQuery(directorySyncQueries.conflicts(canView))
  const identitySettingsQuery = useQuery({
    ...settingsQueries.identity(),
    enabled: canViewLoginSettings && (canCreate || canUpdate) && formOpen,
  })
  const createMutation = useMutation(directorySyncMutations.create(queryClient))
  const updateMutation = useMutation(directorySyncMutations.update(queryClient))
  const validateMutation = useMutation(directorySyncMutations.validate(queryClient))
  const syncMutation = useMutation(directorySyncMutations.sync(queryClient))
  const cancelMutation = useMutation(directorySyncMutations.cancel(queryClient))
  const resolveMutation = useMutation(directorySyncMutations.resolveConflict(queryClient))
  const retryEventMutation = useMutation(directorySyncMutations.retryEvent(queryClient))
  const previewMutation = useMutation({ mutationFn: directorySyncApi.preview })
  const filtered = useManagementTextFilter(connections, search, (item) => [
    item.name,
    item.providerType,
    item.status,
    item.policy.mode,
  ])

  const confirmPeople = (onConfirmed: () => void) => {
    modal.confirm({
      title: '确认开启人员同步？',
      content: '开启后目录同步可能创建或更新用户、登录身份和组织成员关系。组织同步不受此开关影响。',
      okText: '确认开启',
      cancelText: '保持关闭',
      onOk: onConfirmed,
    })
  }

  const writing = useRef(false)
  const previewing = useRef(false)
  const validating = useRef(false)
  const writeMutations = [
    createMutation,
    updateMutation,
    syncMutation,
    cancelMutation,
    resolveMutation,
    retryEventMutation,
  ]
  const writesBlocked = writeMutations.some(
    (mutation) => mutation.isPending || writeOutcomeUnknown(mutation.error),
  )
  const saveError =
    createMutation.submittedAt > updateMutation.submittedAt
      ? createMutation.error
      : updateMutation.error
  const saveBlocked = writeOutcomeUnknown(saveError)
  const perform = async (action: () => Promise<unknown>) => {
    if (writing.current || writesBlocked || connectionsQuery.isError || !canView) return
    writing.current = true
    try {
      await action()
    } catch {
      /* The persistent feedback owns the original request error. */
    } finally {
      writing.current = false
    }
  }
  const runSync = (connection: DirectoryConnection) =>
    void perform(async () => {
      await syncMutation.mutateAsync(connection.id)
      setSelectedId(connection.id)
      setRuntimeTab('runs')
      setPreviewOpen(false)
    })
  const loadPreview = async (connection: DirectoryConnection) => {
    if (previewing.current) return
    previewing.current = true
    if (previewConnection?.id !== connection.id) setPreview(null)
    setPreviewConnection(connection)
    setPreviewOpen(true)
    try {
      setPreview(await previewMutation.mutateAsync(connection.id))
    } catch {
      /* Read feedback stays in the preview. */
    } finally {
      previewing.current = false
    }
  }
  const openForm = (record: DirectoryConnection | null) => {
    if (writing.current || (writesBlocked && !saveBlocked)) return
    if (!saveBlocked) {
      createMutation.reset()
      updateMutation.reset()
      setEditing(record)
    }
    setFormOpen(true)
  }
  const feedback = (
    <MutationFeedback
      operations={[
        {
          mutation: createMutation,
          label: '创建目录连接',
          target: createMutation.variables?.name,
          receipt: { message: '目录连接已创建' },
        },
        {
          mutation: updateMutation,
          label: '更新目录连接',
          target: updateMutation.variables?.id,
          receipt: { message: '目录连接已更新' },
        },
        {
          mutation: syncMutation,
          label: '目录同步',
          target: syncMutation.variables,
          receipt: {
            message: '已返回同步运行记录',
            evidence: (
              <Space>
                <Text copyable>{syncMutation.data?.id}</Text>
                <StatusTag value={syncMutation.data?.status ?? 'unknown'} />
                <Button
                  size="small"
                  onClick={() => {
                    setSelectedId(syncMutation.variables ?? '')
                    setRuntimeTab('runs')
                  }}
                >
                  查看运行记录
                </Button>
              </Space>
            ),
          },
        },
        {
          mutation: cancelMutation,
          label: '取消同步',
          target: cancelMutation.variables,
          receipt: { message: '已提交取消请求，请核对运行记录' },
        },
        {
          mutation: retryEventMutation,
          label: '重试目录事件',
          target: retryEventMutation.variables?.eventId,
          receipt: { message: '已返回事件记录，请核对处理状态' },
        },
        {
          mutation: resolveMutation,
          label: '处理目录冲突',
          target: resolveMutation.variables?.id,
          receipt: { message: '冲突处理请求已完成' },
        },
      ]}
    />
  )
  const columns: TableColumnsType<DirectoryConnection> = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 180,
      render: (value: string) => <Text strong>{value}</Text>,
    },
    {
      title: '目录类型',
      dataIndex: 'providerType',
      width: 110,
      render: (value: string) => <Tag>{providerLabels[value] ?? value}</Tag>,
    },
    { title: '组织同步', width: 100, render: () => <BooleanTag value trueLabel="已开启" /> },
    {
      title: '人员同步',
      width: 100,
      render: (_value, record) => (
        <BooleanTag value={record.policy.syncPeople} trueLabel="已开启" falseLabel="未开启" />
      ),
    },
    {
      title: '同步方式',
      width: 120,
      render: (_value, record) => modeLabels[record.policy.mode] ?? record.policy.mode,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '最近同步',
      dataIndex: 'lastRunAt',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      fixed: 'right',
      width: 180,
      render: (_value, record) => (
        <Space size={2}>
          <ManagementIconButton
            aria-label="查看同步记录"
            tooltip="同步记录"
            icon={<EyeOutlined />}
            onClick={() => setSelectedId(record.id)}
          />
          {canSync ? (
            <ManagementIconButton
              aria-label="预览变更"
              tooltip="预览变更"
              icon={<CloudSyncOutlined />}
              loading={previewMutation.isPending && previewMutation.variables === record.id}
              disabled={previewMutation.isPending}
              onClick={() => void loadPreview(record)}
            />
          ) : null}
          {canValidate ? (
            <ManagementIconButton
              aria-label="验证连接"
              tooltip="验证连接"
              icon={<SafetyCertificateOutlined />}
              loading={validateMutation.isPending && validateMutation.variables === record.id}
              disabled={validateMutation.isPending}
              onClick={async () => {
                if (validating.current) return
                validating.current = true
                try {
                  await validateMutation.mutateAsync(record.id)
                } catch {
                  /* Feedback is persistent. */
                } finally {
                  validating.current = false
                }
              }}
            />
          ) : null}
          {canUpdate ? (
            <ManagementIconButton
              aria-label="编辑目录连接"
              tooltip="编辑"
              icon={<EditOutlined />}
              onClick={() => openForm(record)}
            />
          ) : null}
          {canSync ? (
            <ManagementIconButton
              aria-label="立即同步"
              tooltip="立即同步"
              icon={<CloudSyncOutlined />}
              disabled={writesBlocked || connectionsQuery.isError}
              loading={syncMutation.isPending && syncMutation.variables === record.id}
              onClick={() => runSync(record)}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  if (!canView)
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看目录同步的权限。" />
      </div>
    )

  return (
    <AccessManagementTablePage
      resourceName="目录同步"
      columns={columns}
      createAction={
        canCreate ? (
          <Button
            aria-label={saveBlocked ? '查看待核对草稿' : '新增目录连接'}
            size="small"
            type="primary"
            icon={<PlusOutlined />}
            disabled={
              (writesBlocked && !saveBlocked) || readableQueryData(connectionsQuery) === undefined
            }
            onClick={() => openForm(null)}
          >
            {saveBlocked ? '查看待核对草稿' : '新增目录连接'}
          </Button>
        ) : null
      }
      error={connectionsQuery.error}
      hasReadData={connectionsQuery.data !== undefined}
      dataSource={filtered}
      rowKey="id"
      loading={connectionsQuery.isLoading}
      refreshing={connectionsQuery.isFetching}
      onRefresh={() => void connectionsQuery.refetch()}
      placeholder="搜索连接、目录类型、状态或同步方式"
      searchKeyword={search}
      setSearchKeyword={setSearch}
    >
      {feedback}
      <OperationFeedback
        label="验证目录连接"
        target={validateMutation.variables}
        pending={validateMutation.isPending}
        error={validateMutation.error}
        readOnly
        receipt={
          validateMutation.isSuccess
            ? {
                message:
                  validateMutation.data?.message ??
                  (validateMutation.data?.valid ? '连接验证通过' : '连接验证未通过'),
              }
            : undefined
        }
      />
      <DirectoryConnectionModal
        canManagePeople={canManagePeople}
        confirm={confirmPeople}
        connection={editing}
        loading={createMutation.isPending || updateMutation.isPending}
        loginProviders={
          (canViewLoginSettings
            ? readableQueryData(identitySettingsQuery)?.providers
            : undefined) ?? []
        }
        feedback={
          <>
            <ReadFeedback query={connectionsQuery} label="目录连接" />
            {canViewLoginSettings ? (
              <ReadFeedback query={identitySettingsQuery} label="登录源目录" />
            ) : (
              <ManagementState
                compact
                kind="no-permission"
                description="当前账号没有登录源目录查看权限。LDAP / SCIM 等独立凭据配置仍可填写。"
              />
            )}
            <OperationFeedback
              label="保存目录连接"
              target={editing?.id ?? createMutation.variables?.name}
              pending={createMutation.isPending || updateMutation.isPending}
              error={saveError}
            />
          </>
        }
        blocked={writesBlocked || connectionsQuery.isError}
        loginProvidersUnavailable={!canViewLoginSettings || identitySettingsQuery.isError}
        preserveDraft={saveBlocked}
        loginProvidersLoading={identitySettingsQuery.isLoading}
        open={formOpen && readableQueryData(connectionsQuery) !== undefined}
        onCancel={() => {
          if (!writing.current) setFormOpen(false)
        }}
        onSubmit={(input: DirectoryConnectionInput) =>
          void perform(async () => {
            if (
              (['feishu', 'wecom', 'dingtalk'].includes(input.providerType) &&
                (!canViewLoginSettings ||
                  identitySettingsQuery.isError ||
                  identitySettingsQuery.isPending)) ||
              saveBlocked
            )
              return
            if (editing) await updateMutation.mutateAsync({ id: editing.id, input })
            else await createMutation.mutateAsync(input)
            setFormOpen(false)
          })
        }
      />
      <ChangePreviewModal
        loading={previewMutation.isPending}
        open={previewOpen && readableQueryData(connectionsQuery) !== undefined}
        target={previewConnection?.name}
        blocked={writesBlocked || previewMutation.isError}
        feedback={
          <>
            <OperationFeedback
              label="读取同步预览"
              readOnly
              pending={previewMutation.isPending}
              error={previewMutation.error}
            />
            {syncMutation.variables === previewConnection?.id ? feedback : null}
            {previewMutation.isError && previewConnection ? (
              <Button size="small" onClick={() => void loadPreview(previewConnection)}>
                重试读取预览
              </Button>
            ) : null}
          </>
        }
        preview={
          previewMutation.error &&
          readableQueryData({ data: preview, error: previewMutation.error }) === undefined
            ? null
            : preview
        }
        syncing={syncMutation.isPending}
        onClose={() => setPreviewOpen(false)}
        onSync={() => {
          if (previewConnection && preview?.connectionId === previewConnection.id)
            runSync(previewConnection)
        }}
      />
      <Modal
        title={selected ? `${selected.name} · 运行与事件` : '运行与事件'}
        width={960}
        open={Boolean(selected)}
        footer={null}
        styles={{ header: { minHeight: 32 }, title: visuallyHiddenModalTitleStyle }}
        onCancel={() => setSelectedId('')}
      >
        {feedback}
        <Space wrap>
          <Button
            size="small"
            onClick={() =>
              void Promise.all([
                runsQuery.refetch(),
                runtimeStatusQuery.refetch(),
                eventsQuery.refetch(),
                conflictsQuery.refetch(),
              ])
            }
          >
            刷新运行记录
          </Button>
          <Text type="secondary">刷新仅重读记录，不会发起同步。</Text>
        </Space>
        <Tabs
          activeKey={runtimeTab}
          onChange={setRuntimeTab}
          items={[
            {
              key: 'runtime',
              label: '实时事件',
              children: selected ? (
                <>
                  <ReadFeedback query={runtimeStatusQuery} label="目录事件状态" />
                  <ReadFeedback query={eventsQuery} label="目录事件" />
                  <DirectoryRuntimePanel
                    canRetry={canSync && !writesBlocked}
                    connection={selected}
                    events={readableQueryData(eventsQuery) ?? []}
                    eventsLoading={eventsQuery.isLoading}
                    eventsUnavailable={
                      eventsQuery.isError && readableQueryData(eventsQuery) === undefined
                    }
                    retryingEventId={
                      retryEventMutation.isPending
                        ? retryEventMutation.variables?.eventId
                        : undefined
                    }
                    status={readableQueryData(runtimeStatusQuery)}
                    statusUnavailable={
                      runtimeStatusQuery.isError &&
                      readableQueryData(runtimeStatusQuery) === undefined
                    }
                    statusLoading={runtimeStatusQuery.isLoading}
                    onRetry={(eventId) =>
                      void perform(() =>
                        retryEventMutation.mutateAsync({ connectionId: selected.id, eventId }),
                      )
                    }
                  />
                </>
              ) : null,
            },
            {
              key: 'runs',
              label: '运行历史',
              children: (
                <>
                  <ReadFeedback query={runsQuery} label="目录同步记录" />
                  <DirectoryRunHistory
                    canSync={canSync && !writesBlocked && !runsQuery.isError}
                    loading={runsQuery.isLoading}
                    unavailable={runsQuery.isError && readableQueryData(runsQuery) === undefined}
                    runs={readableQueryData(runsQuery) ?? []}
                    onCancel={() =>
                      selected && void perform(() => cancelMutation.mutateAsync(selected.id))
                    }
                  />
                </>
              ),
            },
            {
              key: 'conflicts',
              label: `冲突 (${readableQueryData(conflictsQuery) ? readableQueryData(conflictsQuery)!.filter((item) => item.connectionId === selected?.id && item.status === 'open').length : '—'})`,
              children: (
                <>
                  <ReadFeedback query={conflictsQuery} label="目录冲突" />
                  <DirectoryConflictCenter
                    canManagePeople={canManagePeople && !writesBlocked && !conflictsQuery.isError}
                    loading={conflictsQuery.isLoading}
                    unavailable={
                      conflictsQuery.isError && readableQueryData(conflictsQuery) === undefined
                    }
                    conflicts={(readableQueryData(conflictsQuery) ?? []).filter(
                      (item) => item.connectionId === selected?.id,
                    )}
                    onResolve={(id, resolution) =>
                      void perform(() => resolveMutation.mutateAsync({ id, resolution }))
                    }
                  />
                </>
              ),
            },
          ]}
        />
      </Modal>
    </AccessManagementTablePage>
  )
}
